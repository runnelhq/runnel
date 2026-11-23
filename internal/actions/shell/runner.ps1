# Start script of shell.powershell (ADR 0023): reads the parameters as JSON from stdin,
# runs the script given as first argument with them (splatting) and turns an exception
# into a message on stderr and exit code 1. Written by Runnel; do not edit.
$ErrorActionPreference = 'Stop'
$utf8 = New-Object System.Text.UTF8Encoding $false
[Console]::OutputEncoding = $utf8
$OutputEncoding = $utf8
$params = @{}
$raw = [Console]::In.ReadToEnd()
if ($raw.Trim()) {
  foreach ($p in ($raw | ConvertFrom-Json).PSObject.Properties) { $params[$p.Name] = $p.Value }
}
$global:LASTEXITCODE = 0
try {
  & $args[0] @params
} catch {
  [Console]::Error.WriteLine($_.Exception.Message)
  exit 1
}
exit $global:LASTEXITCODE
