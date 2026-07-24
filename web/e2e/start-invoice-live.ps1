$ErrorActionPreference = 'Stop'
$webRoot = Split-Path -Parent $PSScriptRoot
$repoRoot = Split-Path -Parent $webRoot
Push-Location $webRoot
try {
  bun run build
} finally {
  Pop-Location
}
Push-Location $repoRoot
try {
  go run -ldflags=-checklinkname=0 ./web/e2e/invoice-live-server
} finally {
  Pop-Location
}
