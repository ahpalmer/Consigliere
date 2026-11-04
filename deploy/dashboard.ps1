<#
Opens the dashboard of the agent running in AKS: signs in if needed, tunnels a local port to the pod with
kubectl port-forward, copies the sign-in token (Web:Token) to the clipboard and opens the browser. Ctrl+C closes the
tunnel and clears the token from the clipboard.

    deploy/dashboard.ps1 [-Port 8081]
#>
param([int]$Port = 8081)
$ErrorActionPreference = 'Stop'

$context = 'aks-consigliere'
$namespace = 'consigliere'

# kubectl, kubelogin and az aren't always on PATH; these are where Docker Desktop and the Azure CLI install them.
foreach ($dir in @("$HOME\.azure-kubelogin", "$HOME\.azure-kubectl", "$env:ProgramFiles\Docker\Docker\resources\bin",
        "$env:ProgramFiles\Microsoft SDKs\Azure\CLI2\wbin")) {
    if ((Test-Path $dir) -and ($env:PATH -split ';') -notcontains $dir) { $env:PATH += ";$dir" }
}
foreach ($tool in 'kubectl', 'kubelogin') {
    if (-not (Get-Command $tool -ErrorAction SilentlyContinue)) { throw "$tool not found; see docs/deploy.md" }
}

function Get-ReadyPods {
    $ready = kubectl --context $context -n $namespace get deployment/consigliere -o 'jsonpath={.status.readyReplicas}' 2>&1
    if ($LASTEXITCODE -ne 0) { return $null }
    return "$ready"
}

# The cluster takes the Azure CLI's sign-in, so an expired one is fixed with az login.
Write-Host 'Checking the agent...'
$ready = Get-ReadyPods
if ($null -eq $ready) {
    Write-Host 'Azure sign-in needed. Sign in with the account that owns the subscription.'
    az login --only-show-errors | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'az login failed' }
    $ready = Get-ReadyPods
    if ($null -eq $ready) { throw "can't reach the cluster; try: kubectl --context $context -n $namespace get pods" }
}
if ($ready -ne '1') { Write-Warning 'The agent pod is not ready; the dashboard may not load.' }

function Test-Port([int]$p) {
    $client = New-Object System.Net.Sockets.TcpClient
    try { return $client.ConnectAsync('127.0.0.1', $p).Wait(300) -and $client.Connected } catch { return $false }
    finally { $client.Dispose() }
}
while (Test-Port $Port) { $Port++ }

# Same lookup as config.DefaultSecretsPath.
$secretsFile = $env:CONSIGLIERE_SECRETS
if (-not $secretsFile) {
    $secretsFile = "$env:APPDATA\Consigliere\secrets.json"
    if (-not (Test-Path $secretsFile)) {
        $secretsFile = "$env:APPDATA\Microsoft\UserSecrets\dotnet-Consigliere.Host-141f7b7f-9bfd-4e1b-979b-27c15113afee\secrets.json"
    }
}
$token = $null
if (Test-Path $secretsFile) { $token = (Get-Content -Raw $secretsFile | ConvertFrom-Json).'Web:Token' }

$tunnel = Start-Process kubectl -ArgumentList "--context $context -n $namespace port-forward svc/consigliere ${Port}:8080" `
    -NoNewWindow -PassThru
try {
    $deadline = (Get-Date).AddSeconds(30)
    while (-not (Test-Port $Port)) {
        if ($tunnel.HasExited) { throw 'the tunnel closed before it was ready (see the kubectl error above)' }
        if ((Get-Date) -gt $deadline) { throw 'the tunnel did not open within 30 seconds' }
        Start-Sleep -Milliseconds 300
    }
    if ($token) {
        Set-Clipboard -Value $token
        Write-Host 'Sign-in token copied to the clipboard: paste it if the dashboard asks.'
    } else {
        Write-Warning "No Web:Token in $secretsFile; copy it from there by hand."
    }
    Start-Process "http://localhost:$Port"
    Write-Host "Dashboard: http://localhost:$Port   (Ctrl+C to close the tunnel)"
    while (-not $tunnel.HasExited) { Start-Sleep -Milliseconds 500 }
} finally {
    if (-not $tunnel.HasExited) { $tunnel.Kill() }
    if ($token -and (Get-Clipboard) -eq $token) { Set-Clipboard -Value $null }
    Write-Host 'Tunnel closed.'
}
