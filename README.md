# 🎮 Backend Challenge — Processamento Distribuído de Apostas em Go

Serviço de alta performance construído em **Go** com **Uber Fx**, **PostgreSQL (pgx nativo)**, **AWS SQS FIFO** e **Keycloak OAuth 2.0 / OIDC** para processamento concorrente de apostas e carteiras de jogadores com consistência financeira estrita.

---

## 🚀 Principais Características

- **Precisão Decimal Exata:** Sem uso de `float32` ou `float64` em qualquer etapa (`domain.Money` encapsulado).
- **Controle de Concorrência Pessimista:** `SELECT ... FOR NO KEY UPDATE` previne *lost updates* em disputas simultâneas de saldo.
- **Idempotência Persistente:** Hash canônico do payload com deduplicação no PostgreSQL e retorno do resultado original.
- **Ledger Imutável (Append-Only):** Invariantes matemáticas validadas no domínio e protegidas por trigger nativa no PostgreSQL.
- **Padrões Inbox & Transactional Outbox:** Publicação confiável de eventos e deduplicação no consumo SQS sem dupla movimentação.
- **Resolução de Reversões Fora de Ordem:** Tratamento resiliente de `PENDING_REFERENCE` com background worker e backoff exponencial.
- **Ciclo de Vida Limpo com Uber Fx:** Processos isolados (`cmd/api` e `cmd/worker`) com graceful shutdown coordenado.

---

## 📋 Pré-requisitos

- **Go 1.26+** (ou a versão instalada no seu ambiente)
- **Docker** & **Docker Compose**
- **Git**

---

## 🛠️ Configuração e Execução

Você pode executar a aplicação de duas formas: **Totalmente via Docker** (tudo automatizado em containers) ou **Manual / Local** (infraestrutura no Docker e serviços Go rodando na sua máquina).

---

### Opção 1: Execução Total via Docker (Recomendada)

Com um único comando, o Docker Compose compila as imagens Go multi-stage, provisiona o PostgreSQL, MiniStack (filas SQS FIFO e DLQ), Keycloak (com realm e clientes pré-configurados), executa as migrações do banco de dados e inicia os serviços `api` e `worker`:

```bash
# 1. Configurar variáveis de ambiente
cp .env.example .env

# 2. Compilar e subir todos os serviços (Infra + API + Worker)
docker compose up --build
```

Caso prefira rodar em background (detached mode):
```bash
docker compose up -d --build

# Acompanhar logs dos serviços da aplicação:
docker compose logs -f api worker

# Para encerrar todos os serviços e containers:
docker compose down
```

Serviços e portas disponíveis:
- **API HTTP:** `http://localhost:8080`
- **Keycloak IdP:** `http://localhost:8081` (Painel Admin: `admin` / `admin`)
- **MiniStack (AWS SQS):** `http://localhost:4566`
- **PostgreSQL 15:** `localhost:5432` (User/Password: `postgres` / `postgres`, DB: `jungle_gaming`)

---

### Opção 2: Execução Manual / Desenvolvimento Local (Go CLI)

Ideal para desenvolvimento ativo, depuração e execução passo a passo no seu ambiente local:

#### 1. Iniciar apenas a infraestrutura auxiliar:
```bash
docker compose up -d postgres ministack keycloak migrate
```

#### 2. Configurar variáveis de ambiente:
```bash
cp .env.example .env
```

#### 3. Iniciar os serviços Go nos terminais:

- **Terminal 1 — Iniciar a API HTTP:**
  ```bash
  go run cmd/api/main.go
  ```
  A API iniciará na porta `8080`.

- **Terminal 2 — Iniciar os Background Workers (SQS, Outbox e Retries):**
  ```bash
  go run cmd/worker/main.go
  ```

---

## 🧪 Testes

### Testes Unitários
Executa todos os testes de domínio, regras de negócio e mapeamento:

```bash
go test -count=1 -race ./...
```

### Testes de Integração e Concorrência Real
Executa o conjunto de testes contra o banco PostgreSQL real (concorrência de 50 apostas simultâneas, disputa de 2x 80.00 sobre saldo de 100.00, imutabilidade do ledger, pendências e reconciliação):

```bash
DATABASE_URL="postgres://postgres:postgres@localhost:5432/jungle_gaming?sslmode=disable" \
go test -count=1 -tags=integration -race ./tests/integration/...
```

### Verificação Estática de Código
```bash
go vet ./...
```

### Testes Automatizados Ponta a Ponta (Postman / Newman)
A coleção Postman v2.1 para validação ponta a ponta da API está localizada em `deployments/postman/jungle-gaming.postman_collection.json`. Ela cobre a sequência completa: Health checks, Keycloak OIDC, criação interna de carteira com saldo, apostas (`BET`, `WIN`, `LOSS`, `REFUND`, erro por saldo insuficiente, replay e conflito de idempotência), isolamento B2B e reconciliação matemática.

Pode ser executada importando no Postman ou via terminal com o Newman:
```bash
newman run deployments/postman/jungle-gaming.postman_collection.json
```

---

## 📚 Endpoints da API

### 1. Health Checks (Públicos)
```bash
# Liveness
curl http://localhost:8080/health/live

# Readiness (valida conexão com PostgreSQL e SQS)
curl http://localhost:8080/health/ready

# Métricas Operacionais
curl http://localhost:8080/metrics
```

### 2. Autenticação (Obter Token no Keycloak)
```bash
# Obter token como provider-a
TOKEN=$(curl -s -X POST "http://localhost:8081/realms/jungle-gaming/protocol/openid-connect/token" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "grant_type=client_credentials&client_id=provider-a&client_secret=provider-a-secret" | jq -r .access_token)

# Obter token de serviço interno
INTERNAL_TOKEN=$(curl -s -X POST "http://localhost:8081/realms/jungle-gaming/protocol/openid-connect/token" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "grant_type=client_credentials&client_id=internal&client_secret=internal-secret" | jq -r .access_token)
```

### 3. Abertura de Carteira (Operação Interna)
```bash
curl -X POST http://localhost:8080/wallets \
  -H "Authorization: Bearer $INTERNAL_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "playerId": "0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1",
    "initialBalance": { "amount": "1000.00", "currency": "BRL" }
  }'
```

### 4. Envio de Operação de Aposta (`BET`)
```bash
curl -X POST http://localhost:8080/wagering/transactions \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: provider-a:transaction-101" \
  -d '{
    "providerId": "provider-a",
    "externalTransactionId": "transaction-101",
    "playerId": "0192f28f-5dc0-7d58-bdb2-814ad6a0f4a1",
    "walletId": "<WALLET_ID>",
    "roundId": "round-987",
    "gameId": "fortune-tiger",
    "kind": "BET",
    "money": { "amount": "25.00", "currency": "BRL" }
  }'
```

### 5. Consulta Paginada do Ledger
```bash
curl "http://localhost:8080/wallets/<WALLET_ID>/ledger?limit=10" \
  -H "Authorization: Bearer $TOKEN"
```

### 6. Reconciliação Matemática de Saldo
```bash
curl -X POST "http://localhost:8080/wallets/<WALLET_ID>/reconciliation" \
  -H "Authorization: Bearer $TOKEN"
```

---

## 🏛️ Detalhes da Arquitetura

Consulte o arquivo [`ARCHITECTURE.md`](./ARCHITECTURE.md) para a documentação detalhada sobre:
- Padrões de precisão numérica
- Resolução de idempotência persistente
- Concorrência pessimista com `FOR NO KEY UPDATE`
- Máquina de estados das transações
- Resolução assíncrona de referências pendentes
- Ciclo de vida de desligamento com Uber Fx
