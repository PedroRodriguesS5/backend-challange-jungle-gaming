#!/usr/bin/env bash
set -euo pipefail

echo "===================================================="
echo " 1. Verificando formatação (gofmt) e análise (go vet)"
echo "===================================================="
UNFORMATTED=$(gofmt -l .)
if [ -n "$UNFORMATTED" ]; then
  echo "❌ Arquivos fora do padrão gofmt:"
  echo "$UNFORMATTED"
  exit 1
fi
go vet ./...
echo "✅ Código formatado e go vet aprovado!"

echo ""
echo "===================================================="
echo " 2. Executando Testes Unitários (com -race)"
echo "===================================================="
go test -v -count=1 -race ./...
echo "✅ Testes unitários aprovados!"

echo ""
echo "===================================================="
echo " 3. Subindo Infraestrutura e Serviços no Docker"
echo "===================================================="
if [ ! -f .env ]; then
  cp .env.example .env
fi

docker compose up -d --build

echo "⏳ Aguardando API ficar pronta em http://localhost:8080/health/ready ..."
for i in {1..30}; do
  if curl -sf http://localhost:8080/health/ready > /dev/null; then
    echo "✅ API e dependências (PostgreSQL + SQS) prontas!"
    break
  fi
  if [ "$i" -eq 30 ]; then
    echo "❌ Timeout aguardando a API inicializar."
    docker compose logs
    exit 1
  fi
  sleep 2
done

echo ""
echo "===================================================="
echo " 4. Executando Testes de Integração e Concorrência"
echo "===================================================="
export DATABASE_URL="postgres://postgres:postgres@localhost:5432/jungle_gaming?sslmode=disable"
export AWS_ENDPOINT_URL="http://localhost:4566"
export KEYCLOAK_ISSUER_URL="http://localhost:8081/realms/jungle-gaming"

go test -v -count=1 -tags=integration -race ./tests/integration/...
echo "✅ Testes de integração e concorrência aprovados!"

echo ""
echo "===================================================="
echo " 5. Executando Suíte E2E (Postman / Newman)"
echo "===================================================="
if command -v npx >/dev/null 2>&1; then
  npx --yes newman run deployments/postman/jungle-gaming.postman_collection.json
  echo "✅ Testes E2E do Postman aprovados!"
else
  echo "⚠️ npx/Node.js não encontrado; pulando etapa opcional do Newman."
fi

echo ""
echo "🎉 TODOS OS TESTES PASSARAM COM SUCESSO!"