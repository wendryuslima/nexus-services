# Imagens em mensagens

O Nexus aceita uma imagem JPEG ou PNG por mensagem, com legenda opcional de até
4.000 caracteres Unicode. O upload passa pela API; os bytes ficam em bucket
privado do Garage e o MongoDB guarda a referência e os metadados verificados.

## Preparação local

Preencha manualmente as variáveis `GARAGE_*` no `.env`, usando o `.env.example`
como referência. Use valores aleatórios independentes: `GARAGE_ACCESS_KEY_ID`
com prefixo `GK` seguido de 32 caracteres hexadecimais, e
`GARAGE_SECRET_ACCESS_KEY` e `GARAGE_RPC_SECRET` com 64 caracteres hexadecimais cada.

Com o `.env` da aplicação configurado:

```powershell
docker compose up -d --build
```

O Compose inicia Garage 2.4.1 em modo de nó único, com
volumes persistentes para dados e metadados. O próprio Garage cria o layout, a
chave e o bucket privado na primeira inicialização.

| Variável | Uso |
| --- | --- |
| `GARAGE_ENDPOINT` | Endpoint S3 usado pelo backend; `http://localhost:3900` fora do Docker. No Compose, `http://garage:3900`. |
| `GARAGE_PUBLIC_ENDPOINT` | Origem alcançável pelo navegador, usada na assinatura. Local: `http://localhost:3900`. HTTPS obrigatório com `APP_ENV=production`. |
| `GARAGE_REGION` | `garage`, igual à região em `deploy/garage.toml`. |
| `GARAGE_BUCKET` | `nexus-images` por padrão. |
| `GARAGE_ACCESS_KEY_ID` / `GARAGE_SECRET_ACCESS_KEY` | Credenciais S3 do backend e da configuração inicial do Garage. |
| `GARAGE_RPC_SECRET` | Segredo hexadecimal de 32 bytes usado pelo Garage. |
| `GARAGE_PORT` | Porta local da API S3, `3900` por padrão. |

A assinatura inclui o host: não substitua `garage:3900` por outro domínio depois
de assinar. Configure o endpoint público correto antes de gerar a URL. A porta
3900 está publicada apenas em loopback no Compose local. Em produção, exponha a
API S3 por proxy HTTPS e use um cluster com replicação e backups. O modo de nó
único deste Compose é destinado ao desenvolvimento. Alterar as variáveis de
credenciais depois do provisionamento não substitui uma rotação coordenada de
chaves no Garage.

## Upload: `POST /v1/images`

Requer sessão e a mesma proteção de origem das outras rotas de escrita.
`Content-Type: multipart/form-data`, com exatamente estes campos:

| Campo | Conteúdo |
| --- | --- |
| `chatId` | Conversa da qual o usuário participa. |
| `clientUploadId` | UUID gerado uma vez pelo frontend e reutilizado nas tentativas do mesmo upload. |
| `file` | Um arquivo JPEG ou PNG. |

O navegador deve definir o boundary; não configure manualmente o header
`Content-Type` ao enviar `FormData`.

Limites: 10 MiB de arquivo, até 8192 pixels por lado e até 16 milhões de pixels.
A API identifica o formato pelos bytes, lê a configuração e decodifica a imagem
inteira para rejeitar arquivos inválidos. O nome e o MIME fornecidos pelo cliente
não determinam o formato nem a chave no storage. O checksum SHA-256 corresponde
aos bytes armazenados. As imagens são preservadas sem reprocessamento ou remoção
de EXIF; o usuário deve considerar que os metadados originais fazem parte do arquivo.

Há dois uploads simultâneos por instância, arquivos temporários privados e limite
distribuído por usuário de 30 tentativas em 10 segundos e 60 por minuto. Os
contadores de upload são separados dos contadores de envio de mensagem. Os
timeouts HTTP configurados no servidor também se aplicam ao upload; aumente
`HTTP_READ_TIMEOUT` / `HTTP_WRITE_TIMEOUT` conforme a rede e o proxy utilizados.

Criação e retry concluídos retornam `200 OK`:

```json
{
  "data": {
    "id": "image-id",
    "contentType": "image/png",
    "sizeBytes": 123456,
    "width": 1280,
    "height": 720
  }
}
```

O par `(uploaded_by, client_upload_id)` possui índice único. Mesmos bytes e chat
recuperam o mesmo `id`, inclusive após a imagem ser vinculada à mensagem. Outro
conteúdo ou chat retorna `409 image_upload_conflict`. Upload simultâneo retorna
`409 image_upload_in_progress` com `Retry-After`. Um upload expirado retorna
`410 image_upload_expired`; uma nova intenção exige novo `clientUploadId`.

## Envio da mensagem

Use a rota existente `POST /v1/chats/{chatId}/messages`:

```json
{
  "clientMessageId": "018f92d4-2c47-7c87-b33e-d2ea331d2ff2",
  "content": {
    "type": "IMAGE",
    "imageId": "image-id",
    "caption": "Legenda opcional"
  }
}
```

`IMAGE` não aceita `value`. `TEXT` continua exigindo `value` e não aceita referência
de imagem ou legenda. `imageId` e `caption` são preservados nas respostas HTTP,
timeline, payload da outbox e `message.created` do WebSocket. URLs temporárias e
bytes não trafegam nos eventos.

A transação MongoDB muda condicionalmente a imagem de `READY` para `ATTACHED`,
verificando proprietário, chat e prazo, aloca a sequência e insere mensagem e
outbox. A imagem só pode pertencer a uma mensagem. Falha em qualquer etapa
reverte todas as alterações. `409 image_unavailable` indica imagem expirada,
inexistente, de outro proprietário/chat ou já vinculada a outra mensagem.

O retry compara `chatId`, remetente, tipo, `imageId` e legenda. Repita o mesmo
`clientMessageId` e o mesmo conteúdo após uma resposta perdida. Upload e envio
são operações separadas; se o usuário desistir antes do envio, o arquivo será
recolhido pela limpeza.

## Leitura: `GET /v1/images/{imageId}/access`

Retorna os metadados acima mais `url` e `expiresAt`. A URL assinada permite
`GetObject` durante dez minutos e o navegador baixa diretamente do Garage.
Enquanto a imagem está `READY`, somente o remetente tem acesso à prévia.
Após `ATTACHED`, os participantes autorizados do chat podem solicitar a URL.
Usuários sem acesso recebem `404 image_not_found`. O bucket não é público.

Uma URL assinada pode ser compartilhada e utilizada durante sua validade sem
nova autenticação no Nexus. Mudanças na participação do chat impedem novas URLs;
não revogam automaticamente URLs já emitidas ou cópias já baixadas. Respostas de
acesso e objetos usam `Cache-Control: private, no-store` ou `no-store`.

O frontend mantém a URL apenas em memória, renova antes da expiração enquanto a
aba está ativa e permite nova tentativa quando a imagem falha. Sessões distintas
não compartilham a mesma chave de cache. Para exibição por `<img>`, não é preciso
habilitar CORS no bucket; um futuro download por `fetch` exigirá CORS apropriado.

## Consistência e limpeza

Estados: `PENDING → READY → ATTACHED` ou `PENDING/READY → DELETING → DELETED`.
MongoDB e Garage não compartilham uma transação. O registro com object key existe
antes do PUT, permitindo rastrear inclusive uma resposta de storage perdida.
Uploads possuem lease de dois minutos, token de propriedade e PUT limitado a
30 segundos. Só o detentor do token pode confirmar ou liberar a tentativa.

Cada API executa uma rotina a cada minuto, com lote de até 100 operações. Claims
atômicos impedem conflito entre instâncias. Imagens sem vínculo expiram após
24 horas e só são reservadas para exclusão sem lease ativo. A vinculação de uma
mensagem e a reserva para exclusão disputam a mesma mudança de estado no MongoDB.
`ATTACHED` nunca é elegível à limpeza.

Falhas de exclusão deixam o trabalho em `DELETING` para retry após o lease.
Uma segunda exclusão é executada após cinco minutos para cobrir PUTs tardios ou
interrompidos; um `DELETED` mantém o identificador como tombstone. Não há TTL nos
registros de imagem, pois apagar metadados antes de excluir bytes perderia o
rastreamento. Suspensões de processo além de todos os prazos e alterações manuais
no storage exigem reconciliação operacional. Uma política de retenção para
imagens enviadas e para tombstones deve ser definida separadamente.

## Validação e rollout

Os testes incluem arquivos inválidos e limites, idempotência, leases, rollback,
retries concorrentes, autorização de leitura e envio, S3 privado e o caminho
HTTP → MongoDB/outbox → NATS → WebSocket → URL assinada → Garage.

Para executar a suíte de integração, forneça `MONGODB_TEST_URI` para um replica
set de testes, `NATS_TEST_URL` para um JetStream isolado e `GARAGE_TEST_ENDPOINT`,
`GARAGE_TEST_ACCESS_KEY_ID`, `GARAGE_TEST_SECRET_ACCESS_KEY` e opcionalmente
`GARAGE_TEST_BUCKET`. A suíte E2E usa `nexus-images`. Execute `go test ./...`.
Não use o NATS de produção: o teste configura `CHAT_EVENTS`. Sem essas variáveis,
os testes de integração correspondentes são ignorados.

Atualize frontend, publisher e dispatcher antes de habilitar envio por APIs novas;
consumidores antigos validam somente `TEXT`. O contrato continua na versão 1,
com a união de conteúdo ampliada para `TEXT | IMAGE`.

Referências: [Garage 2.4.1 — configuração inicial](https://github.com/deuxfleurs-org/garage/blob/v2.4.1/doc/book/quick-start/_index.md),
[compatibilidade S3](https://garagehq.deuxfleurs.fr/documentation/reference-manual/s3-compatibility/).
