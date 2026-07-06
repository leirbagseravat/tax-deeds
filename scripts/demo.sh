#!/usr/bin/env bash
# End-to-end demo: upload a matrícula PDF, wait for the pipeline
# (ingest → OCR → LLM extraction), then walk the read API.
#
# Prerequisites — everything running locally:
#   docker compose up -d          # postgres + fake-gcs (+ ocr-stub)
#   make migrate
#   LLM_PROVIDER=stub make run    # or a real ANTHROPIC_API_KEY
#
# Usage: scripts/demo.sh [path/to/file.pdf]
set -euo pipefail

API=${API_URL:-http://localhost:8080}
PDF=${1:-testdata/matricula.pdf}
TIMEOUT=${TIMEOUT:-60}

say()  { printf '\n\033[1m== %s ==\033[0m\n' "$*"; }
show() { python3 -m json.tool <<<"$1"; }

jsonval() { # jsonval <json> <key>
  python3 -c 'import sys,json; print(json.loads(sys.argv[1]).get(sys.argv[2], ""))' "$1" "$2"
}

say "Upload $PDF"
UPLOAD=$(curl -sf -F "file=@$PDF" "$API/api/v1/documents")
show "$UPLOAD"
DOC_ID=$(jsonval "$UPLOAD" id)

say "Poll until extracted (timeout ${TIMEOUT}s)"
for _ in $(seq 1 "$TIMEOUT"); do
  STATUS=$(jsonval "$(curl -sf "$API/api/v1/documents/$DOC_ID")" status)
  printf 'status=%s\n' "$STATUS"
  case "$STATUS" in
    extracted) break ;;
    failed)
      curl -s "$API/api/v1/documents/$DOC_ID" | python3 -m json.tool
      echo "pipeline failed" >&2
      exit 1
      ;;
  esac
  sleep 1
done
if [ "$STATUS" != extracted ]; then
  echo "timed out waiting for extraction (last status: $STATUS)" >&2
  exit 1
fi

say "Full matrícula aggregate"
MATRICULA=$(curl -sf "$API/api/v1/documents/$DOC_ID/matricula")
show "$MATRICULA"
MAT_ID=$(jsonval "$MATRICULA" id)

say "Proprietários + cadeia dominial"
show "$(curl -sf "$API/api/v1/matriculas/$MAT_ID/proprietarios")"

say "Averbações only (?kind=averbacao)"
show "$(curl -sf "$API/api/v1/matriculas/$MAT_ID/atos?kind=averbacao")"

say "Active liens (?status=ativo)"
show "$(curl -sf "$API/api/v1/matriculas/$MAT_ID/onus?status=ativo")"

say "Demo complete — document $DOC_ID, matrícula $MAT_ID"
