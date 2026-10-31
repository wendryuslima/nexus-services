# Contrato HTTP — Mensagens

Este documento descreve o contrato público disponível para o frontend criar mensagens em um chat existente.

## Visão geral

| Operação | Método | Rota | Autenticação | Sucesso |
| --- | --- | --- | --- | --- |
| Criar mensagem | `POST` | `/v1/chats/{chatId}/messages` | Obrigatória | `201` ou `200` |

A sessão é enviada pelo navegador em cookies `HttpOnly`. O frontend deve usar `credentials: "include"` no Fetch ou `withCredentials: true` no Axios. Não leia nem envie JWTs manualmente.

As respostas JSON usam:

```http
Content-Type: application/json; charset=utf-8
Cache-Control: no-store
```

---

## `POST /v1/chats/{chatId}/messages`

Persiste uma mensagem de texto e cria, na mesma transação, o registro interno que permitirá sua entrega futura. A resposta confirma apenas a persistência; ela não confirma que outro usuário recebeu ou leu a mensagem.

### Parâmetro de rota

| Parâmetro | Tipo | Descrição |
| --- | --- | --- |
| `chatId` | string | Identificador do chat no qual a mensagem será criada. O usuário autenticado precisa participar dele. |

O backend retorna o mesmo `404 chat_not_found` tanto para um chat inexistente quanto para um chat ao qual o usuário não possui acesso.

### Corpo da requisição

```json
{
  "clientMessageId": "018f92d4-2c47-7c87-b33e-d2ea331d2ff2",
  "content": {
    "type": "TEXT",
    "value": "Olá!"
  }
}
```

| Campo | Tipo | Obrigatório | Regras |
| --- | --- | --- | --- |
| `clientMessageId` | string | Sim | UUID canônico, diferente do UUID nulo. Deve ser gerado uma vez pelo cliente e reutilizado em retries da mesma mensagem. |
| `content` | object | Sim | Conteúdo da mensagem. |
| `content.type` | string | Sim | `TEXT` ou `IMAGE`. |
| `content.value` | string | Para `TEXT` | Texto UTF-8 não vazio, com no máximo 4.000 caracteres Unicode. |
| `content.imageId` | string | Para `IMAGE` | ID de upload pronto, do remetente e desta conversa. |
| `content.caption` | string | Não | Legenda de `IMAGE`, até 4.000 caracteres Unicode. |

Envie os bytes previamente por `POST /v1/images`. Veja o [contrato de imagens](images.md) para upload, leitura e retries. `IMAGE` não aceita `value`; `TEXT` não aceita `imageId` ou `caption`.

Não envie `senderId`, `recipientId`, `sequence`, `createdAt` nem qualquer identificador de usuário. O remetente é sempre extraído da sessão autenticada.

O JSON é estrito: campos desconhecidos, corpo vazio, JSON malformado e mais de um valor JSON são rejeitados. O corpo inteiro possui limite de 20 KiB.

### Limites de envio por usuário

O endpoint aceita até **30 tentativas de envio em 10 segundos** e **60 em 60 segundos**, em janelas deslizantes. Ambos os limites são aplicados simultaneamente por usuário autenticado, somando todos os chats, abas e dispositivos. A contagem é compartilhada entre instâncias da API pelo MongoDB.

Cada tentativa admitida consome uma posição antes de validar o corpo ou persistir a mensagem, inclusive retries idempotentes e requisições que depois falhem. Tentativas rejeitadas com `429` não consomem posições nem prolongam a espera. Esse controle não se aplica a consultas da timeline ou eventos de recebimento via WebSocket.

Ao atingir qualquer limite, o endpoint responde:

```http
HTTP/1.1 429 Too Many Requests
Retry-After: 10
Content-Type: application/json; charset=utf-8
Cache-Control: no-store
```

```json
{
  "error": {
    "code": "message_rate_limit_exceeded",
    "message": "Você enviou mensagens muito rápido. Aguarde 10 segundos."
  }
}
```

`Retry-After` contém o número inteiro de segundos até haver capacidade nas duas janelas, arredondado para cima. O valor é calculado a cada recusa; `10` é apenas um exemplo. O cabeçalho é exposto por CORS para as origens permitidas. O frontend deve pausar os envios pelo prazo informado, manter o texto digitado e reutilizar o mesmo `clientMessageId` ao tentar novamente. Outros dispositivos podem consumir a capacidade liberada nesse intervalo, causando novo `429`.

Se o controle de frequência estiver indisponível, o envio é recusado com `503 message_rate_limit_unavailable` e `Retry-After: 1`, sem criar a mensagem. A coleção `message_rate_limits` e seu índice TTL são preparados na inicialização da API; registros sem atividade expiram automaticamente.

### Resposta — mensagem criada

Status: `201 Created`

```json
{
  "data": {
    "id": "6ab85d4aa94d3539db5a83a6",
    "clientMessageId": "018f92d4-2c47-7c87-b33e-d2ea331d2ff2",
    "chatId": "6ab85d4aa94d3539db5a83a0",
    "sequence": 183,
    "senderId": "6a7521f266ec07731754f78e",
    "content": {
      "type": "TEXT",
      "value": "Olá!"
    },
    "createdAt": "2026-10-13T12:00:00Z",
    "deliveryStatus": "PERSISTED"
  }
}
```

### Resposta — retry idempotente

Status: `200 OK`

O formato do corpo é idêntico ao da resposta `201`. Isso significa que uma tentativa anterior com o mesmo `clientMessageId` já foi persistida e o backend devolveu a mensagem original.

O frontend deve tratar `200` e `201` como sucesso. A notificação `message.created` atualiza diretamente a timeline e a posição/data da conversa conhecida; o sucesso do POST não dispara GETs adicionais. A implementação atual não insere estado otimista antes da confirmação de persistência.

### Schema da resposta

| Campo | Tipo | Descrição |
| --- | --- | --- |
| `data.id` | string | ID persistido da mensagem. Use-o como identidade definitiva da mensagem. |
| `data.clientMessageId` | string | ID gerado pelo cliente para idempotência e correlação do envio persistido. |
| `data.chatId` | string | ID do chat. |
| `data.sequence` | integer | Ordem crescente e estável da mensagem dentro do chat. |
| `data.senderId` | string | ID do remetente autenticado. |
| `data.content.type` | string | `TEXT` ou `IMAGE`. |
| `data.content.value` | string | Texto persistido de `TEXT`. |
| `data.content.imageId` | string | Referência persistida de `IMAGE`. |
| `data.content.caption` | string | Legenda opcional de `IMAGE`. |
| `data.createdAt` | string | Data RFC 3339 em UTC. |
| `data.deliveryStatus` | string | Estado visível ao remetente. Atualmente sempre `PERSISTED`. |

`PERSISTED` significa que a mensagem e o evento interno de outbox foram confirmados no MongoDB. A notificação em tempo real atualiza a tela, sem confirmar entrega ao destinatário nem leitura.

---

## Idempotência e retries

A chave de idempotência é o par formado por usuário autenticado e `clientMessageId`.

| Situação | Resultado |
| --- | --- |
| Primeira tentativa válida | `201 Created` e nova mensagem. |
| Retry com mesmo `clientMessageId`, chat e conteúdo | `200 OK` e a mensagem original. |
| Mesmo `clientMessageId` com outro chat ou conteúdo | `409 idempotency_conflict`. |

Fluxo recomendado no frontend:

1. Gere `clientMessageId` antes de criar o item otimista.
2. Guarde-o junto do item local enquanto a requisição estiver pendente.
3. Em erro de rede, timeout do cliente ou resposta desconhecida, repita a mesma requisição com o mesmo `clientMessageId`.
4. Em `200` ou `201`, substitua o item local pelos campos retornados, principalmente `id` e `sequence`.
5. Em `409`, não repita com esse ID; trate como falha do item otimista e gere um novo ID apenas se a pessoa optar por reenviar como uma nova mensagem.

Não gere outro `clientMessageId` automaticamente após timeout. O servidor pode ter concluído a transação e perdido apenas a resposta HTTP.

---

## Erros

Todas as respostas de erro usam este envelope:

```json
{
  "error": {
    "code": "invalid_message_content",
    "message": "Informe um conteúdo de mensagem válido."
  }
}
```

O frontend deve tomar decisões por `error.code`. O texto de `error.message` é adequado para exibição, mas pode mudar.

| HTTP | `error.code` | Quando ocorre |
| ---: | --- | --- |
| `400` | `invalid_request` | Corpo vazio, JSON inválido, campo desconhecido ou mais de um valor JSON. |
| `401` | `unauthenticated` | Cookie ausente, token inválido/expirado ou sessão inativa. |
| `403` | `cross_origin_request_rejected` | A política de origem rejeitou a chamada. |
| `404` | `chat_not_found` | Chat inexistente ou usuário sem participação no chat. |
| `405` | `method_not_allowed` | Método diferente de `POST`. A resposta inclui `Allow: POST`. |
| `409` | `idempotency_conflict` | `clientMessageId` já foi usado para outra mensagem lógica. |
| `413` | `request_body_too_large` | Corpo acima de 20 KiB. |
| `415` | `unsupported_media_type` | `Content-Type` diferente de `application/json`. |
| `422` | `invalid_client_message_id` | `clientMessageId` ausente ou inválido. |
| `409` | `image_unavailable` | Imagem inexistente, expirada, já vinculada ou sem permissão para esta mensagem. |
| `422` | `invalid_message_content` | Tipo, valor ou formato do conteúdo inválido. |
| `422` | `message_content_too_long` | Texto acima de 4.000 caracteres Unicode. |
| `429` | `message_rate_limit_exceeded` | Limite de envio por usuário atingido. Aguarde o prazo de `Retry-After`. |
| `503` | `message_rate_limit_unavailable` | Controle de frequência indisponível. Tente novamente após `Retry-After`. |
| `504` | `request_timeout` | A operação excedeu o prazo do servidor. |
| `500` | `internal_error` | Falha inesperada. |

---

## Exemplo com Fetch

```ts
export interface MessageContent {
  type: "TEXT";
  value: string;
}

export interface Message {
  id: string;
  clientMessageId: string;
  chatId: string;
  sequence: number;
  senderId: string;
  content: MessageContent;
  createdAt: string;
  deliveryStatus: "PERSISTED";
}

export interface CreateMessageResponse {
  data: Message;
}

export interface APIErrorResponse {
  error: {
    code: string;
    message: string;
  };
}

export async function createMessage(
  chatId: string,
  clientMessageId: string,
  value: string,
): Promise<Message> {
  const response = await fetch(`${API_URL}/v1/chats/${chatId}/messages`, {
    method: "POST",
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify({
      clientMessageId,
      content: {
        type: "TEXT",
        value,
      },
    }),
  });

  const payload: CreateMessageResponse | APIErrorResponse =
    await response.json();

  if (!response.ok) {
    throw new APIError(payload as APIErrorResponse, response.status);
  }

  return (payload as CreateMessageResponse).data;
}
```

---

## Fora do escopo atual

Este contrato ainda não define:

- edição ou exclusão de mensagens;
- outros anexos além de imagens JPEG/PNG;
- recibos de entrega ou leitura;

A sincronização incremental está disponível no [contrato da timeline](frontend-timeline-contract.md). A notificação `message.created` é entregue a ambos os participantes via WebSocket, incluindo as outras abas do remetente. O envio permanece HTTP e `PERSISTED` continua significando somente persistência confirmada.
