# 📐 Arquitetura do Sistema — Processamento Distribuído de Apostas em Go

Este documento detalha as decisões arquiteturais, garantias financeiras, padrões de concorrência e estratégias de recuperação de falhas implementadas no serviço.

---

## 1. Precisão e Integridade Monetária (`Money`)

### Decisão Técnica
É expressamente proibido o uso de `float32` ou `float64` em qualquer camada do sistema (parsing, cálculo, serialização ou banco de dados), eliminando erros de arredondamento de ponto flutuante binário (IEEE 754).

- **Value Object:** `domain.Money` é um Value Object imutável encapsulado no pacote de domínio.
- **Estrutura Interna:** Utiliza `github.com/shopspring/decimal` com precisão decimal exata e escala truncada em 2 casas decimais.
- **Validações de Entrada:** Rejeita strings vazias, valores `NaN`, `Infinity`, notação científica (ex: `1e2`) e números com mais de duas casas decimais.
- **Compatibilidade de Moeda:** Todas as operações aritméticas (`Add`, `Subtract`, `Compare`) validam a igualdade de moeda ISO 4217, retornando `ErrCurrencyMismatch` quando incompatíveis.
- **Persistência PostgreSQL:** Salvo na coluna `balance` como `NUMERIC(15,2)`, acompanhado da coluna `currency VARCHAR(3)`.

---

## 2. Controle de Concorrência e Transações (Pessimistic Locking)

### Problema: Lost Updates em Operações Concorrentes
Em sistemas de jogos e apostas, múltiplas requisições simultâneas podem atingir a mesma carteira (ex: múltiplas apostas, vitórias e reversões paralelas). Se utilizássemos transações sem trava de linha ou controle puramente otimista em alta contenção, incorreríamos em altas taxas de conflito e retries caros.

### Implementação
1. Ao processar qualquer aposta ou reversão, a aplicação inicia uma transação explícita com `pgx.Tx` via `TxManager`.
2. Executa um **Lock Pessimista de linha** diretamente no PostgreSQL:
   ```sql
   SELECT id, player_id, currency, balance, version, created_at, updated_at
   FROM wallets
   WHERE id = $1
   FOR NO KEY UPDATE;
   ```
3. O modo `FOR NO KEY UPDATE` permite que operações de leitura simples e referências de chave estrangeira continuem sem bloqueio, enquanto enfileira no banco qualquer outro escritor que dispute a mesma carteira.
4. **Isolamento entre Carteiras:** O lock é por linha (`wallet_id`). Carteiras distintas avançam em paralelo com throughput máximo, sem nenhum lock global de tabela ou de aplicação.
5. **Invariante no Banco:** A tabela `wallets` possui constraint nativa `CHECK (balance >= 0)`.

---

## 3. Idempotência Persistente e Isolamento B2B

### Mecânica de Idempotência
1. **Hash Canônico Determinístico:**
   - Todo payload externo tem seus campos de negócio (`providerId`, `externalTransactionId`, `playerId`, `walletId`, `roundId`, `gameId`, `kind`, `amount`, `currency`, `referenceExternalTransactionId`) normalizados em um JSON ordenado e hasheado via SHA-256.
   - Cabeçalhos de transporte e a chave de idempotência são ignorados no cálculo do hash do conteúdo.
2. **Restrição de Unicidade:**
   - A tabela `wager_transactions` impõe a constraint `UNIQUE (provider_id, external_id)`.
3. **Resolução de Conflitos:**
   - Em caso de conflito de chave única (`SQLSTATE 23505`), a transação lê a transação original persistida.
   - Se o hash do payload for idêntico, a operação retorna a resposta de sucesso original com `"idempotentReplay": true` e o saldo registrado na carteira.
   - Se o hash for diferente (reutilização indevida de chave/id para dados diferentes), o sistema retorna HTTP 409 (Conflict).

### Isolamento Multitenant B2B
- Não existe tabela de usuários unificada compartilhada entre provedores.
- A identidade do provedor (`providerId`) é autenticada através do token JWT (`client_id` no OAuth 2.0 / Keycloak).
- Toda consulta e escrita valida que o provedor autenticado seja o proprietário da transação. Nenhum provedor pode consultar ou interferir nas operações de outros provedores.

---

## 4. O Ledger Imutável (Append-Only)

### Invariantes
- A tabela `wallet_ledger_entries` registra todas as movimentações financeiras (`DEBIT` e `CREDIT`).
- Para cada lançamento, o domínio valida a invariante matemática:
  $$\text{balanceAfter} = \text{balanceBefore} \pm \text{amount}$$
- **Proteção Contra Alterações:** Uma trigger PL/pgSQL no banco proíbe qualquer `UPDATE` ou `DELETE`:
  ```sql
  CREATE OR REPLACE FUNCTION prevent_ledger_mutation()
  RETURNS TRIGGER AS $$
  BEGIN
      RAISE EXCEPTION 'wallet_ledger_entries is append-only: updates and deletes are forbidden';
  END;
  $$ LANGUAGE plpgsql;
  ```
- **Reconciliação:** O endpoint `POST /wallets/:walletId/reconciliation` soma todos os créditos e subtrai os débitos do ledger, comparando com o saldo gravado na carteira para atestar integridade matemática absoluta.

---

## 5. Máquina de Estados e Referências Pendentes (`PENDING_REFERENCE`)

### Estados da Transação
- `PENDING`: Criada, aguardando término.
- `PENDING_REFERENCE`: Reversão (`REFUND` ou `ROLLBACK`) recebida antes da aposta original.
- `PROCESSED`: Concluída com sucesso (estado terminal imutável).
- `REJECTED`: Recusada por regra de negócio (saldo insuficiente, referência inválida, etc.; estado terminal).
- `FAILED`: Falha irreversível (estado terminal).

### Resolução de Reversões Fora de Ordem
1. Se um `REFUND` ou `ROLLBACK` chega antes da aposta referenciada, ela não é rejeitada de imediato.
2. O sistema grava a transação com o estado `PENDING_REFERENCE` e emite o evento `WagerTransactionPendingReference`.
3. O background worker `PendingReferenceWorker` varre transações nesse estado utilizando backoff exponencial.
4. Quando a transação original chega e é confirmada, o worker conclui o estorno na mesma transação atômica que movimenta a carteira.
5. Caso o TTL da pendência seja ultrapassado (default 5 minutos), a transação é finalizada como `REJECTED` com o código `REFERENCE_NOT_FOUND`.

---

## 6. Mensageria: Padrões Transactional Outbox e Inbox Deduplication

### Transactional Outbox
1. Todas as mutações de domínio (`WagerTransactionProcessed`, `WalletBalanceChanged`, `WagerTransactionRejected`, `WagerTransactionPendingReference`) são gravadas na tabela `outbox` **dentro da mesma transação SQL** que atualiza a carteira e o ledger.
2. O worker `OutboxWorker` pesquisa eventos pendentes utilizando `SELECT ... FOR UPDATE SKIP LOCKED`, garantindo paralelismo sem contenção entre múltiplos nós.
3. Os eventos são despachados para o AWS SQS FIFO com `MessageGroupId` vinculado ao `walletId` e `MessageDeduplicationId` ao `eventId`.
4. Falhas temporárias sofrem retries com backoff exponencial; após limite de tentativas, são marcadas como `FAILED` para análise na DLQ.

### Inbox Pattern (Deduplicação no Consumidor SQS)
1. Ao consumir uma mensagem da fila SQS, o consumidor registra a tupla `(consumer_name, message_id)` na tabela `inbox`.
2. Essa inserção ocorre dentro da transação de negócio da operação.
3. Se a mensagem já tiver sido processada anteriormente, a violação de unicidade da `inbox` detecta a duplicata com segurança, permitindo o ack da fila sem reprocessar o saldo.

---

## 7. Composição e Ciclo de Vida com Uber Fx (`go.uber.org/fx`)

O projeto utiliza Uber Fx para orquestração de dependências desacopladas e gerenciamento seguro de ciclo de vida:

```text
├── cmd/
│   ├── api/main.go          # Entrypoint da API Web
│   └── worker/main.go       # Entrypoint dos Background Workers (SQS, Outbox, Retry)
├── internal/
│   ├── domain/              # Entidades e Value Objects puros (SEM DEPENDÊNCIA DO FX)
│   ├── usecase/             # Casos de uso orquestrando regras de negócio
│   ├── infra/
│   │   ├── config/          # Carregamento de variáveis de ambiente
│   │   ├── database/        # pgxpool, Repositórios e TxManager
│   │   ├── http/            # Rotas net/http, Middleware JWT e Handlers
│   │   └── messaging/       # SQS, OutboxWorker, SQSConsumer e PendingReferenceWorker
│   └── di/                  # Módulos Fx (ConfigModule, DatabaseModule, UsecaseModule, HTTPModule, MessagingModule)
```

### Isolamento de Processos e Graceful Shutdown
- **Processo API (`cmd/api`)**: Inicializa `ConfigModule`, `DatabaseModule`, `UsecaseModule` e `HTTPModule`. No shutdown (`OnStop`), encerra a escuta HTTP com timeout de 10 segundos para requisições em andamento antes de desconectar o pool de banco.
- **Processo Worker (`cmd/worker`)**: Inicializa `ConfigModule`, `DatabaseModule`, `UsecaseModule` e `MessagingModule`.
- **Ordem de Desligamento Garantida no Worker:**
  1. `OnStop` sinaliza aos workers o cancelamento do polling SQS.
  2. Aguarda os processamentos em voo (`wg.Wait()`) finalizarem seus commits ou rollbacks.
  3. Somente após todos os workers estarem parados, o hook de parada do `DatabaseModule` fecha o pool de conexões do PostgreSQL.
