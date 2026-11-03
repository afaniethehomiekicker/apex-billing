#!/usr/bin/env bash
# Builds APEX Billing into one program: ./apex-billing
set -euo pipefail
cd "$(dirname "$0")"
echo "[1/2] Building the web pages..."
( cd frontend && { [ -d node_modules ] || npm install; } && npm run build )
echo "[2/2] Building the program..."
go build -ldflags "-s -w" -o apex-billing .
echo "Done: ./apex-billing  (copy .env.example to .env and fill it in first)"
