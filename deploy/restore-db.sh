#!/usr/bin/env bash
# Replaces the cluster's database with a local file: the laptop's database on first move to AKS, or a downloaded
# backup. Stops the agent, swaps the file (keeping the old one as consigliere.db.before-restore), and starts it again.
#
#   deploy/restore-db.sh <file.db>      (a relative path: kubectl cp misreads "C:\..." as a pod name)
set -euo pipefail

file=${1:?usage: deploy/restore-db.sh <file.db>}
[[ -f $file ]] || { echo "no such file: $file" >&2; exit 1; }
k() { kubectl -n consigliere "$@"; }

k scale deployment/consigliere --replicas=0
while [[ -n $(k get pods -l app.kubernetes.io/name=consigliere -o name) ]]; do sleep 2; done

# A throwaway pod on the same disk (the disk attaches to one node at a time, so the agent has to be stopped).
k apply -f - <<'EOF'
apiVersion: v1
kind: Pod
metadata:
  name: consigliere-restore
spec:
  securityContext:
    runAsNonRoot: true
    runAsUser: 65532
    runAsGroup: 65532
    fsGroup: 65532
    seccompProfile: { type: RuntimeDefault }
  containers:
    - name: restore
      image: busybox:1.37.0
      command: [sleep, "3600"]
      securityContext:
        allowPrivilegeEscalation: false
        capabilities: { drop: [ALL] }
      volumeMounts:
        - { name: data, mountPath: /data }
  volumes:
    - name: data
      persistentVolumeClaim: { claimName: consigliere-data }
EOF
trap 'k delete pod consigliere-restore --wait=false >/dev/null; k scale deployment/consigliere --replicas=1' EXIT
k wait --for=condition=Ready pod/consigliere-restore --timeout=3m

k cp "$file" consigliere-restore:/data/pack/db/consigliere.db.new
k exec consigliere-restore -- sh -c '
  set -e
  cd /data/pack/db
  if [ -f consigliere.db ]; then mv consigliere.db consigliere.db.before-restore; fi
  rm -f consigliere.db-wal consigliere.db-shm
  mv consigliere.db.new consigliere.db
  ls -l'
echo "Restored $file; starting the agent."
