# Realtime: implementação e operação

O gateway está no processo `cmd/api`, na porta HTTP existente. Mensagens continuam sendo criadas por HTTP e persistidas na mesma transação que a sequência e a outbox. O dispatcher publica o mesmo evento público para os dois participantes; cada API recebe uma assinatura por usuário local e distribui às suas conexões.

## Inicialização

1. Atualize API, outbox publisher e dispatcher junto com o frontend.
2. Configure `NATS_URL`, `HTTP_ALLOWED_ORIGINS` e as variáveis `REALTIME_*` de `.env.example`. O Compose encaminha essas variáveis e mantém o broker interno.
3. O gateway inicia sempre com a API. A API audita os dados antes de iniciar e recusa a inicialização se encontrar timeline legada/inconsistente.
4. No `nexus-modules`, configure `VITE_API_URL`. A sessão autenticada abre o WebSocket automaticamente; não existe flag de ativação no ambiente nem no build.
5. Use frontend/API na mesma origem em produção, proxy `/v1` com upgrade e `wss`. Há um exemplo em `deploy/nginx-realtime.conf`, a ser adaptado ao upstream real.

O contrato de cookies permanece `HttpOnly`, `Secure` no ambiente de produção e caminho `/`. Origens são comparadas por esquema, host e porta canônica, sem wildcard, origem ausente ou `null`. Não há tokens na URL. Os navegadores suportados precisam oferecer Web Locks, BroadcastChannel e WebSocket; ausência de Web Locks impede renovação concorrente insegura.

O WebSocket é parte permanente da aplicação. As variáveis `REALTIME_*` ajustam apenas prazos e limites de recursos. Falha do NATS não derruba as rotas HTTP: sockets afetados fecham com `1013`, e novas conexões esperam o transporte voltar.

## Componentes

- `internal/ports/realtime*`: contrato público, validação e assinatura abstrata.
- `internal/drivers/websocket`: handshake, hub local, buffer de preparação, filas limitadas, heartbeat e encerramento.
- `internal/resources/nats/realtime_subscriber`: uma conexão compartilhada, assinaturas sem queue group, flush de prontidão e sinalização de perdas.
- `GET /v1/auth/session`: consulta sem rotação e sem cache, com usuário e validade do acesso verificado.
- Timeline: carga inicial, histórico e recuperação incremental por sequência, mantendo o cursor legado.
- Frontend: coordenador de credenciais entre abas, provider autenticado, reconexão com jitter e aplicação direta dos eventos ao estado da tela.

A primeira notificação é `connection.ready`. O frontend usa HTTP para carga inicial e recuperação; no caminho normal, `message.created` atualiza diretamente a timeline aberta e a posição/data da conversa conhecida. O POST apenas persiste: seu sucesso não invalida queries. Assim, uma mensagem normal não gera GETs adicionais de chats/timeline nem exibe a faixa de sincronização.

IDs persistidos eliminam duplicatas, e a sessão autenticada determina `isMine`. As mensagens do socket e as atualizações de posição da lista permanecem na projeção da tela até uma resposta HTTP que já as inclua; uma resposta antiga não apaga essas atualizações. Os cursores de paginação continuam sendo fornecidos pelo servidor.

O controlador limita a duas tarefas simultâneas de recuperação. Prontidão/reconexão e lacunas de sequência exigem HTTP; o ponto de recuperação é calculado pela fotografia inicial e pelas sequências contíguas observadas. Durante a primeira carga, eventos são preservados em buffer limitado a 100 itens ou 1 MiB. Excesso provoca reconexão e recuperação. Uma conversa ausente da lista carregada exige uma consulta agrupada para descobrir seus metadados. A API mantém os endpoints incrementais disponíveis.

Conversas não abertas carregam a cauda ao serem selecionadas. `staleTime: 0`, `gcTime: 0` e `refetchOnMount: "always"` evitam retenção entre visitas. O TanStack Query mantém o resultado e os eventos necessários para renderizar uma tela montada, sem persistência local de mensagens. A sessão tem política própria de consulta de identidade. O cliente usa `cache: "no-store"` e as respostas da API usam `Cache-Control: no-store`.

Foco, conectividade e intervalo de 60 segundos com a aba visível recuperam notificações perdidas em segundo plano. As queries não repetem os mesmos gatilhos automaticamente. A faixa de sincronização fica restrita à recuperação inicial, reconexão e lacunas. Falhas de releitura são exibidas e repetidas.

## Validade e encerramento

- `4001`: expiração do acesso, inclusive durante a preparação. O cliente consulta/renova a sessão e abre outra conexão.
- `4003`: sessão expirada/revogada. O cliente descarta dados privados.
- `1013`: NATS, validação de sessão indisponível, fila cheia ou preparação malsucedida. O cliente reconecta e recupera por HTTP.
- `1008`/`1009`: comando do cliente não suportado ou limite de leitura excedido. Não há reconexão automática contínua.
- `1001`: shutdown. O gateway bloqueia reservas, fecha sockets, aguarda suas rotinas e libera assinaturas antes de terminar HTTP.

A revalidação em produção consulta as sessões agrupadas em uma única leitura MongoDB com prazo compartilhado, inicialmente 60s + 5s. A expiração do acesso e o prazo conhecido da sessão têm timers próprios. O Compose reserva 45s para drenagem de sockets, shutdown HTTP e liberação das dependências; os defaults de cada etapa ficam em 10s.

O logout suspende processamento/reconexões antes do HTTP. Sucesso limpa caches e sinaliza outras abas; falha informa que a revogação não foi confirmada. Gerações de sessão e transporte impedem callbacks antigos de atualizar a identidade atual.

## Dados legados e migração

A auditoria de inicialização é somente leitura e usa snapshot. Ela verifica sequências inteiras positivas na faixa segura, contiguidade por chat, chave do cliente e correspondência com `chats.last_sequence`, inclusive chats com contador sem mensagens. Antes da auditoria, as leituras por sequência verificam documentos legados; após uma auditoria bem-sucedida, usam o caminho indexado. Todos os escritores devem preservar o contrato e a transação de persistência.

Se a auditoria recusar os dados:

1. Não inicie a nova versão da API; faça backup e interrompa todos os escritores e workers antes da manutenção dos dados.
2. Inventarie os documentos afetados. Quando o chat já tiver sequências válidas/contíguas, preserve-as. Para chats sem ordem confiável, defina ordem determinística por `(created_at, _id)` e revise a correspondência de eventos pendentes antes de reprocessá-los.
3. Prepare a transformação em uma cópia do banco: atribua sequências `1..N`, restaure os `client_message_id` disponíveis e gere identificadores de migração apenas quando não houver identificador original. Ajuste `chats.last_sequence` para `N`.
4. Planeje a recriação coordenada do índice único de sequência se houver renumeração; não tente trocar valores concorrendo com escritores ou ignorar colisões de índices.
5. Atualize os payloads pendentes/reprocessáveis conforme a identidade persistida. Eventos antigos sem `clientMessageId` continuam sendo aceitos; o cliente deduplica pelo ID persistido.
6. Valide unicidade, contiguidade, participação e recuperação completa; repita a auditoria antes de iniciar os serviços atualizados e reabrir escritores.

Nenhuma migração é executada automaticamente na API, e a implementação não altera dados existentes para inventar contiguidade.

## Testes

```powershell
go test ./...

# MongoDB deve ser replica set; use bancos/serviços destinados a teste.
$env:MONGODB_TEST_URI = "mongodb://127.0.0.1:27129/?directConnection=true&replicaSet=rs0"
$env:NATS_TEST_URL = "nats://127.0.0.1:44229"
go test ./... -count=1 -timeout=120s
```

Sem as variáveis, as integrações externas são puladas. Os testes criam bancos isolados para transações; o teste completo usa o stream `CHAT_EVENTS` e um consumer exclusivo `E2E_DELIVERY_*` em um broker de teste dedicado. Nunca aponte `NATS_TEST_URL` para o broker de produção.

No frontend:

```powershell
bun test
npm run build
```

As verificações cobrem propagação BSON/JSON, compatibilidade legada, falha parcial do dispatcher, recuperação paginada, origem/cookies pelos middlewares reais, primeiro evento, assinatura compartilhada, limites concorrentes, expiração/revogação, shutdown, duas instâncias NATS e o caminho HTTP → MongoDB/outbox → JetStream → dispatcher → sockets de ambos os usuários e de outra aba do remetente.

Os testes do frontend confirmam atualização pelo socket sem GET por mensagem, ausência de sincronização no fluxo normal, deduplicação, proteção contra HTTP antigo, recuperação de lacunas, descarte após logout e repetição de falhas. Os testes de sessão confirmam consulta sem rotação, renovação compartilhada e preservação de falhas de rede.

## Observabilidade e liberação

A API emite agregados `realtime metrics`: conexões, assinaturas, aberturas/rejeições/fechamentos, códigos de fechamento, quantidade/bytes em fila, estouros, eventos e soma da latência de prontidão. Logs de fechamento em debug usam código/motivo estáticos. Nenhum conteúdo, cookie ou token vira log ou label. O frontend apresenta estado de conexão/sincronização e indicador de novas mensagens sem forçar rolagem de quem está lendo o histórico.

Na inicialização, a API registra `realtime gateway started` com o caminho do endpoint. Rejeições de handshake registram status e código, sem credenciais. No console do navegador, filtre por `[WS]`: abertura do socket, `connection.ready`, `message.created`, recuperação HTTP, erros, fechamento e reconexão possuem registros próprios. `Conexão aberta` confirma o transporte; `connection.ready` confirma a prontidão do gateway. No caminho normal, as mensagens exibidas vêm diretamente do evento validado.

Antes de produção, valide o proxy/TLS/cookies no destino, indisponibilidade e retorno do broker, logout entre abas e refresh concorrente em navegadores reais. Defina usuários/abas simultâneos, eventos por segundo e metas de latência; execute carga compatível com esse volume. O limite configurável de 5.000 conexões não representa capacidade medida. Os testes locais não substituem a validação do ambiente de destino nem autorizam uma liberação em produção.
