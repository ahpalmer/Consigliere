package router

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ahpalmer/consigliere/internal/agent"
	"github.com/ahpalmer/consigliere/internal/commands"
	"github.com/ahpalmer/consigliere/internal/llm"
)

type fakeChat struct{ calls int }

func (f *fakeChat) Reply(context.Context, int64, string) (string, error) { f.calls++; return "hi", nil }
func (f *fakeChat) Reset(int64)                                          {}

type fakeSkill struct{ got string }

func (f *fakeSkill) Commands() []commands.Info {
	return []commands.Info{{Name: "/scan", Description: "scan"}}
}
func (f *fakeSkill) Handle(_ context.Context, command, args string, _ int64) (string, error) {
	f.got = command + "|" + args
	return "scanned", nil
}

func newRouter() (*Router, *agent.State, *fakeChat, *fakeSkill) {
	state, chat, skill := agent.NewState(time.Now()), &fakeChat{}, &fakeSkill{}
	return New(state, chat, llm.NewUsageTracker(), skill), state, chat, skill
}

func TestPauseAndResumeToggleTheKillSwitch(t *testing.T) {
	r, state, _, _ := newRouter()
	r.Handle(context.Background(), 1, "/pause")
	if !state.Paused() {
		t.Fatal("not paused")
	}
	r.Handle(context.Background(), 1, "/resume")
	if state.Paused() {
		t.Fatal("still paused")
	}
}

func TestPausedBlocksChatAndSkillCommands(t *testing.T) {
	r, state, chat, skill := newRouter()
	state.Pause()
	for _, text := range []string{"hello", "/scan"} {
		reply, _ := r.Handle(context.Background(), 1, text)
		if !strings.Contains(strings.ToLower(reply), "paused") {
			t.Fatalf("%q -> %q", text, reply)
		}
	}
	if chat.calls != 0 || skill.got != "" {
		t.Fatal("nothing should run while paused")
	}
}

func TestRoutesSkillCommandsWithArgsAndBotSuffix(t *testing.T) {
	r, _, _, skill := newRouter()
	if reply, _ := r.Handle(context.Background(), 1, "/SCAN@Consigliere_HQ_bot  now please "); reply != "scanned" || skill.got != "/scan|now please" {
		t.Fatalf("%q %q", reply, skill.got)
	}
}

func TestStatusAndUnknownCommands(t *testing.T) {
	r, _, chat, _ := newRouter()
	if reply, _ := r.Handle(context.Background(), 1, "/STATUS"); !strings.Contains(reply, "running") {
		t.Fatal(reply)
	}
	if reply, _ := r.Handle(context.Background(), 1, "/nope"); !strings.Contains(reply, "Unknown command") || !strings.Contains(reply, "/scan - scan") {
		t.Fatal(reply)
	}
	if reply, _ := r.Handle(context.Background(), 1, "what's up?"); reply != "hi" || chat.calls != 1 {
		t.Fatal(reply)
	}
}
