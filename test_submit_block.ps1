#!/usr/bin/env powershell

# ブロック投げテスト用スクリプト

$url = "http://localhost:8545/rpc"

$block = @{
    height = 1
    previous_hash = "0x" + ("0" * 64)
    timestamp = 1739700060
    nonce = 12345
    difficulty = 24
    miner = "0x" + ("0" * 40)
    reward = "100"
    transactions = @()
    hash = "0x" + ("0" * 64)
}

$payload = @{
    jsonrpc = "2.0"
    method = "brockchain_submitBlock"
    params = @{
        block = $block
    }
    id = 1
}

$json = $payload | ConvertTo-Json -Depth 10

Write-Host "POST $url"
Write-Host "Body:"
Write-Host $json

$response = Invoke-WebRequest -Uri $url -Method Post -Body $json -ContentType "application/json"
Write-Host "Response:"
$response.Content | ConvertFrom-Json | ConvertTo-Json
