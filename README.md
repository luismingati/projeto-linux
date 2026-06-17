# Projeto Linux — Infraestrutura Financeira Resiliente e Monitorada

Infraestrutura em contêineres para uma pequena aplicação financeira em **Go**,
com proxy reverso + load balancer, duas instâncias da aplicação, banco de dados
persistente e stack de observabilidade (Prometheus + Grafana).

## Arquitetura

```
                         host :8080            host :3000
                             │                     │
                       ┌─────▼─────┐         ┌──────▼──────┐
   cliente  ─────────► │   nginx   │         │   grafana   │
                       │ (LB / RP) │         │  (dashboards)│
                       └─────┬─────┘         └──────┬──────┘
                  round-robin│                      │ PromQL
                   ┌─────────┴─────────┐            │
              ┌────▼────┐         ┌────▼────┐  ┌─────▼──────┐
              │  app1   │         │  app2   │  │ prometheus │
              │ (Go)    │         │ (Go)    │◄─┤  (scrape   │
              └────┬────┘         └────┬────┘  │  /metrics) │
                   └───────┬───────────┘       └────────────┘
                      ┌────▼────┐
                      │   db    │   volume → ./data/postgres
                      │postgres │   (sem porta pública)
                      └─────────┘
        Tudo na rede interna `appnet`. Apenas nginx e grafana têm porta no host.
```

| Serviço      | Imagem                  | Porta no host | Função |
|--------------|-------------------------|---------------|--------|
| `nginx`      | nginx:alpine            | **8080 → 80** | Proxy reverso + load balancer (round-robin) |
| `app1`/`app2`| build `./app` (scratch) | —             | API financeira em Go, idênticas |
| `db`         | postgres:17-alpine      | — (interno)   | Banco com persistência em volume |
| `cadvisor`   | cadvisor:v0.49.1        | — (interno)   | Métricas de recurso por contêiner (CPU/mem/rede) |
| `prometheus` | prom/prometheus         | — (interno)   | Coleta de métricas |
| `grafana`    | grafana/grafana         | **3000**      | Visualização (dashboards provisionados) |

> **Banco de dados nunca exposto:** `db` não declara `ports:`, então só é
> acessível dentro da rede `appnet`. O único ponto de entrada da aplicação é o
> Nginx; o Grafana é publicado apenas para que o dashboard possa ser visto.

## Como rodar

Pré-requisitos: **Docker** + **Docker Compose v2** em execução.

```bash
./start-infra.sh          # valida o Docker, builda e sobe tudo (-d), mostra status
```

Outros comandos:

```bash
./start-infra.sh status   # status dos contêineres
./start-infra.sh logs app1 # acompanha logs (de um serviço, opcional)
./start-infra.sh down     # para a stack (dados do Postgres são preservados)
./start-infra.sh reset    # APAGA tudo: volumes + ./data/postgres (DB do zero no próximo up)
```

> `reset` pede confirmação; use `./start-infra.sh reset -y` para pular o prompt.
> Ele remove os volumes nomeados e o bind mount do banco, então o próximo `up`
> recria o Postgres e re-executa o `db/init.sql`.

Rodar `./start-infra.sh` novamente com a stack já no ar é **idempotente**: os
serviços aparecem como `up-to-date` e o status é reimpresso (nada é recriado).

### Portas em conflito

As portas no host têm padrão `8080` (Nginx) e `3000` (Grafana), mas são
sobrescrevíveis caso já estejam em uso:

```bash
GRAFANA_PORT=3001 NGINX_PORT=8081 ./start-infra.sh
```

## A API

Duas rotas de negócio (valores sempre em **centavos**, inteiros):

### `GET /accounts/{id}/balance`
Retorna o saldo atual e as 10 transações mais recentes.

```bash
curl -s http://localhost:8080/accounts/1/balance | jq
```
```json
{
  "account_id": 1,
  "balance_cents": 1500,
  "transactions": [
    { "id": 2, "amount_cents": 500, "type": "debit",  "created_at": "2026-06-04T12:00:10Z" },
    { "id": 1, "amount_cents": 2000, "type": "credit", "created_at": "2026-06-04T12:00:00Z" }
  ]
}
```

Conta inexistente → `404`.

### `POST /accounts/{id}/transactions`
Aplica um crédito ou débito. A conta é **criada automaticamente** (saldo 0) na
primeira vez que recebe um POST.

```bash
# crédito
curl -s -X POST http://localhost:8080/accounts/1/transactions \
  -H 'Content-Type: application/json' \
  -d '{"amount_cents": 2000, "type": "credit"}' | jq

# débito
curl -s -X POST http://localhost:8080/accounts/1/transactions \
  -H 'Content-Type: application/json' \
  -d '{"amount_cents": 500, "type": "debit"}' | jq
```

Respostas:

| Situação | Código |
|----------|--------|
| Aplicado | `201 Created` |
| JSON / `amount_cents` / `type` inválido | `400 Bad Request` |
| Débito maior que o saldo | `422 Unprocessable Entity` (`insufficient funds`) |

A proteção contra saldo negativo é feita de forma **atômica no banco**
(`UPDATE ... WHERE balance >= amount`), portanto é correta mesmo com as duas
instâncias recebendo POSTs concorrentes via load balancer.

## Observabilidade

- **Métricas** (`/metrics` em cada app, formato Prometheus via `client_golang`):
  - `http_requests_total{route,method,status}`
  - `http_request_duration_seconds` (histograma)
  - `transactions_total{type,result}`
  - métricas de runtime do Go / processo (automáticas)
- **Recursos por contêiner** (`cAdvisor`): expõe CPU, memória, rede e disco de
  **todos** os contêineres (não só das apps) em formato Prometheus — é o
  "`docker stats` dentro do Grafana".
- **Prometheus** coleta de `app1:8080`, `app2:8080`, `cadvisor:8080` (e dele
  mesmo) a cada 15s.
- **Grafana** em <http://localhost:3000> (**admin / admin**). O datasource
  Prometheus e dois dashboards já vêm provisionados:
  - *Finance App Overview*:
    - **Request rate per instance** — visualiza o load balancer ao vivo (app1 vs app2);
    - **Request latency heatmap** — distribuição de latência ao longo do tempo;
    - **Success rate (2xx)** e **Rejection rate (422)** — gauges de saúde;
    - request rate / p95 por rota, transações por tipo/resultado, runtime do Go.
  - *Container Resources (cAdvisor)*: CPU, memória (working set) e rede (RX/TX)
    **por contêiner** — para o monitoramento de recursos sob carga.

## Gerador de carga (`loadgen/`)

Ferramenta auxiliar em Go (**só std lib**, módulo separado) que simula vários
"usuários virtuais" concorrentes batendo no Nginx — enche o banco e faz os
dashboards do Grafana ganharem vida. Mix realista: ~35% leituras, ~40% créditos,
~25% débitos (alguns débitos viram `422` de propósito).

```bash
cd loadgen
go run . -users 1000 -workers 50 -duration 30s
```

Flags:

| Flag | Padrão | Descrição |
|------|--------|-----------|
| `-target`   | `http://localhost:8080` | URL do load balancer (Nginx) |
| `-users`    | `1000` | nº de contas distintas (ids `1..users`) |
| `-workers`  | `50`   | usuários virtuais concorrentes (goroutines) |
| `-duration` | `30s`  | duração da geração de carga |
| `-rps`      | `0`    | limite global de req/s (`0` = o mais rápido possível) |
| `-seed`     | `true` | dá um crédito inicial a cada conta antes da carga |

Imprime RPS ao vivo (1×/s) e, no fim, um resumo com contagem por status,
créditos/débitos e percentis de latência (p50/p90/p95/p99/max). Abra o Grafana
durante a execução para ver as métricas subindo.

## Persistência

Os dados do Postgres ficam no bind mount `./data/postgres` (host). Eles
**sobrevivem** a `docker compose down` e à remoção do contêiner — recriar a
stack reaproveita o mesmo diretório. O `db/init.sql` roda apenas quando o
diretório está vazio (primeiro boot).

## Podman / Red Hat (SELinux)

Em hosts com SELinux, o bind mount do banco pode dar *permission denied*.
Soluções:

1. Adicione a flag de relabel `:Z` no volume do `db` em `docker-compose.yml`:
   ```yaml
   - ./data/postgres:/var/lib/postgresql/data:Z
   ```
   (`:Z` é ignorado sem efeito em Docker/macOS, então é seguro deixar fixo.)
2. Ou relabele o diretório manualmente:
   ```bash
   mkdir -p data/postgres
   chcon -Rt container_file_t data/postgres
   ```
Com Podman, troque `docker compose` por `podman compose` no `start-infra.sh`.

## Estrutura

```
.
├── docker-compose.yml          # infraestrutura completa
├── start-infra.sh              # automação (validação + up + status)
├── README.md
├── app/                        # aplicação Go (std lib + pgx + client_golang)
│   ├── Dockerfile              # multi-stage: golang:1.26 → scratch
│   ├── main.go server.go handlers.go store.go model.go metrics.go
│   └── go.mod go.sum
├── db/init.sql                 # schema (rodado no primeiro boot)
├── nginx/nginx.conf            # upstream + load balancing
├── prometheus/prometheus.yml   # scrape config
├── grafana/provisioning/       # datasource + dashboard automáticos
└── loadgen/                    # gerador de carga (Go, std lib, módulo à parte)
    └── main.go client.go stats.go go.mod
```
