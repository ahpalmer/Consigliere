<#
Opens a bot's dashboard over Tailscale: copies its sign-in token (Web:Token, read from the bot's Key Vault and never
shown) to the clipboard, opens https://<bot>.<tailnet>.ts.net, and clears the clipboard a minute later.

    deploy/dashboard.ps1 [-ResourceGroup rg-consigliere-alison]

Needs the Azure CLI signed in and Tailscale connected on this machine.
#>
param([string]$ResourceGroup = 'rg-consigliere')
$ErrorActionPreference = 'Stop'

$outputs = az deployment group show -g $ResourceGroup -n consigliere --query '[properties.outputs.vmName.value, properties.outputs.keyVaultName.value]' -o tsv
if ($LASTEXITCODE -ne 0) { throw "couldn't read the infra outputs for $ResourceGroup (az login?)" }
$vm, $vault = $outputs

$tailscale = Join-Path $env:ProgramFiles 'Tailscale\tailscale.exe'
$status = & $tailscale status --json | ConvertFrom-Json
if ($status.BackendState -ne 'Running') { throw 'Tailscale is not connected on this machine' }
$url = "https://$vm.$($status.MagicDNSSuffix)"

$secrets = az keyvault secret show --vault-name $vault --name consigliere-secrets --query value -o tsv | ConvertFrom-Json
$secrets.'Web:Token' | Set-Clipboard
Remove-Variable secrets
Start-Process $url
Write-Host "Opened $url. The sign-in token is on the clipboard for 60 seconds."
Start-Sleep 60
Set-Clipboard -Value ' '
