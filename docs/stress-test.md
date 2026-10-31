# Teste de estresse de mensagens

Execute a partir da raiz de `nexus-services`, com Go instalado e a API, MongoDB, NATS, outbox publisher e delivery dispatcher em execução no ambiente que deseja medir:

```powershell
go run ./scripts/stress
```

O padrão usa `http://localhost:8080`, **100 usuários**, **2 minutos de envio**, texto de **256 caracteres** e intervalo de **1,1 segundo por usuário**. Cada usuário tem seu próprio cookie jar e sessão. Os usuários são distribuídos em pares: 100 usuários escrevem em 50 chats, com ambos os participantes enviando para a mesma conversa. Quantidades ímpares conectam o último usuário ao primeiro.

O script cadastra contas `stress-<execução>-<índice>@example.test`, faz login e cria os chats via HTTP. A preparação tem concorrência limitada a quatro usuários para separar o custo de hashing de senhas da carga de mensagens. Todos os remetentes iniciam juntos após essa etapa. O tempo de preparação fica separado no relatório.

As contas, sessões, mensagens e eventos de outbox permanecem no banco; os textos incluem o identificador da execução. Use um banco destinado a testes para poder descartar esses dados depois. Senhas são aleatórias e não são salvas no relatório. Cada execução cria novas contas.

## Cenários

### Carga contínua com 100 usuários

```powershell
go run ./scripts/stress -users 100 -duration 3m -interval 1100ms
```

A carga oferecida é de aproximadamente 91 tentativas por segundo, enquanto o servidor responde dentro do intervalo. Uma requisição fica em andamento por usuário; respostas mais lentas reduzem a vazão oferecida. Compare execuções com os mesmos parâmetros para observar a evolução da latência. O gerador não reproduz uma taxa fixa de chegada independente da capacidade do servidor.

### Rajadas com espera automática após 429

```powershell
go run ./scripts/stress -users 100 -duration 2m -interval 50ms
```

Ao receber `429 message_rate_limit_exceeded`, cada usuário aguarda o `Retry-After` antes de tentar uma nova mensagem. O script aceita segundos ou uma data HTTP; cabeçalho ausente/inválido resulta em espera de um segundo. Cada tentativa usa um novo UUID, porque o teste mede mensagens novas, sem retries automáticos de mensagens que sofreram timeout.

### Pressão sobre o limitador

```powershell
go run ./scripts/stress -users 100 -duration 30s -interval 0s -ignore-rate-limit
```

Mantém até 100 requisições simultâneas e continua enviando depois de `429`. Esse cenário mede também autenticação e controle de frequência: recusas não executam a transação de criação da mensagem. Muitos `429` com latência baixa não demonstram capacidade de gravação no banco. Observe `created_per_second` e `persisted_requests_latency` separadamente.

Com os limites atuais, 100 usuários podem admitir até 3.000 tentativas numa rajada inicial e 6.000 por janela de 60 segundos. A média sustentada admitida se aproxima de 100 tentativas por segundo; erros de validação ou persistência também consomem posições. Para aumentar a carga de gravação além desse teto, aumente `-users` em execuções posteriores.

### Outra API ou mensagens maiores

```powershell
go run ./scripts/stress -base-url http://localhost:8080 -users 100 -duration 2m -message-size 4000 -output stress-results/texto-4000.json
```

Use `-origin http://localhost:5172` se quiser enviar explicitamente a origem configurada na API. O script mantém os cookies retornados por ela; cookies `Secure` exigem HTTPS. Certificados TLS são validados. A configuração padrão não envia `Origin` e não reproduz o preflight do navegador.

Liste os parâmetros com:

```powershell
go run ./scripts/stress -h
```

## Resultados

O terminal mostra progresso a cada cinco segundos. O relatório completo é salvo em `stress-results/<execução>.json`, pasta ignorada pelo Git, ou no caminho definido em `-output`.

| Campo | Interpretação |
| --- | --- |
| `created` / `created_per_second` | Mensagens novas confirmadas com `201`, após validar ID, sequência, chat e chave do cliente na resposta. |
| `idempotent` | Respostas `200` que confirmaram mensagem existente. O gerador usa IDs novos, então normalmente deve ser zero. |
| `attempts_per_second` | Vazão total, incluindo recusas e falhas. |
| `rate_limited` / `rate_limited_percent` | Respostas `429` do limitador de mensagens; contabilizadas separadamente de falhas inesperadas. |
| `failures` / `failure_percent` | Erros de transporte, timeouts, respostas inválidas e códigos HTTP inesperados. |
| `http_statuses` / `errors` | Distribuição dos códigos HTTP e categorias de falha. Falhas antes de receber resposta não têm código HTTP. |
| `persisted_requests_latency` | Média, p50, p95, p99 e máximo dos POSTs confirmados com `200/201`. |
| `rate_limited_requests_latency` | Latência das recusas `429`, separada das gravações. |
| `all_requests_latency` | Latência de todas as tentativas, incluindo falhas. |
| `setup_seconds` / `elapsed_seconds` | Preparação separada do tempo observado de carga; este inclui a conclusão das requisições em andamento. |

As latências são medidas no cliente até a leitura do corpo. Percentis usam histogramas com resolução de 1 ms e memória limitada pelo timeout configurado. O script para de iniciar requisições após `-duration` e aguarda as já iniciadas por até `-request-timeout`. `Ctrl+C` cancela requisições e esperas e salva um relatório parcial se a fase de envio já começou. Falha na preparação encerra antes da medição. Erros inesperados ou interrupção resultam em código de saída 1; somente `429` resulta em código 0.

As sessões não são renovadas durante o teste. A preparação mais a fase de carga deve caber no tempo de vida do acesso configurado na API. Respostas `401` indicam um problema de sessão e são contabilizadas como falhas, sem esconder o resultado com logins automáticos.

## Acompanhar os gargalos

Em outro terminal, acompanhe CPU, memória e I/O:

```powershell
docker stats
```

E os logs dos serviços:

```powershell
docker compose logs --follow --since 1m api outbox-publisher delivery-dispatcher
```

Compare p95/p99 dos envios persistidos, vazão de mensagens criadas, timeouts e erros `500/503/504` com o consumo da API e do MongoDB. Se aumentar usuários elevar a latência e os erros sem elevar a vazão, há sinal de saturação. O consumo da máquina que executa o gerador também influencia o resultado; para medir capacidade do servidor, execute o gerador em outra máquina.

No MongoDB, acompanhe também eventos pendentes e a idade do mais antigo, usando uma conexão administrativa já autenticada no banco de teste:

```javascript
db.outbox_events.countDocuments({ status: { $in: ["PENDING", "PROCESSING"] } })
db.outbox_events.find({ status: { $in: ["PENDING", "PROCESSING"] } }, { created_at: 1, status: 1 }).sort({ created_at: 1 }).limit(1)
db.outbox_events.countDocuments({ status: "FAILED" })
```

Backlog e idade que continuam crescendo apontam atraso na publicação da outbox. O script mede o caminho HTTP até a persistência de mensagem e outbox; os workers processam os eventos normalmente. Ele não abre WebSockets e não mede entrega ao destinatário, fan-out, tempo do JetStream nem recibos de leitura. O relatório HTTP sozinho não identifica qual consulta ou componente provocou a lentidão; use-o em conjunto com as métricas dos serviços.

### Concorrência do publisher

Cada instância usa `OUTBOX_CONCURRENCY=4` por padrão. Para comparar 1, 2, 4 e 8 publicações simultâneas, ajuste essa variável no ambiente do Compose e recrie somente o publisher antes de cada execução:

```powershell
$env:OUTBOX_CONCURRENCY = "4"
docker compose up -d --build --no-deps outbox-publisher
```

A inicialização cria os índices parciais antes de iniciar as publicações. A recuperação de leases expirados é independente da carga, com intervalo padrão de 5 segundos e lote de 100 eventos (`OUTBOX_RECOVERY_INTERVAL` e `OUTBOX_RECOVERY_BATCH_SIZE`). Aguarde a drenagem da fila ou use uma base equivalente antes de comparar execuções; backlog anterior altera o resultado.

Com uma conexão administrativa ao banco de teste, valide a seleção de pendentes:

```javascript
db.outbox_events.find({
  status: "PENDING",
  published_at: null,
  next_attempt_at: { $lte: new Date() }
}).sort({ next_attempt_at: 1, created_at: 1, _id: 1 }).limit(1).explain("executionStats")
```

Verifique o uso de `outbox_pending_schedule`, a ausência de ordenação bloqueante (`SORT`) e poucos documentos/chaves examinados mesmo com backlog. Compare também os logs reais de `findAndModify`, pois o `explain` não representa a disputa entre workers. Meça publicações por segundo e idade do pendente mais antigo junto com p95/p99 HTTP, espera por conexão, duração da transação/commit e CPU/I/O do MongoDB. A melhora da outbox, isoladamente, não comprova a causa da lentidão da API.

## Validar o gerador

```powershell
go test ./scripts/stress -count=1
```

Os testes usam uma API HTTP simulada, sem criar contas nem enviar carga ao ambiente em execução. Cobrem 100 usuários independentes, simultaneidade, compartilhamento de chats, respeito ao `Retry-After`, sobrecarga, separação das latências, falhas e interrupção.
