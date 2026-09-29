# Contrato HTTP — Timeline do chat

## Listar a timeline

```http
GET /v1/chats/{chatId}/timeline
```

A rota exige a sessão autenticada. O usuário recebe `404 chat_not_found` tanto quando o chat não existe quanto quando não participa dele.

### Query

| Parâmetro | Tipo | Padrão | Regra |
| --- | --- | --- | --- |
| `pageSize` | integer | `50` | Entre `1` e `100`. |
| `cursorId` | string | — | Enviar junto com `cursorCreatedAt`. |
| `cursorCreatedAt` | RFC 3339 | — | Enviar junto com `cursorId`. |

A primeira requisição não envia cursor. Para carregar mensagens mais antigas, reutilize exatamente os dois campos de `nextCursor`:

```http
GET /v1/chats/6ab85d4aa94d3539db5a83a0/timeline?pageSize=50&cursorId=6ab85d4aa94d3539db5a83a6&cursorCreatedAt=2026-09-27T00%3A03%3A46.056Z
```

### Resposta — `200 OK`

```json
{
  "data": {
    "items": [
      {
        "id": "6ab85d4aa94d3539db5a83a6",
        "kind": "MESSAGE",
        "createdAt": "2026-09-27T00:03:46.056Z",
        "updatedAt": "2026-09-27T00:03:46.056Z",
        "message": {
          "senderId": "6a7521f266ec07731754f78e",
          "isMine": false,
          "content": {
            "type": "TEXT",
            "value": "Bom dia! Tudo bem?"
          }
        }
      }
    ],
    "pagination": {
      "hasNext": true,
      "nextCursor": {
        "id": "6ab85d4aa94d3539db5a83a6",
        "createdAt": "2026-09-27T00:03:46.056Z"
      },
      "pageSize": 50
    }
  }
}
```

`items` sempre vem como array, inclusive quando vazio. Os itens de cada página vêm em ordem cronológica crescente. A primeira página contém os itens mais recentes; uma página obtida com `nextCursor` contém itens mais antigos e deve ser inserida antes dos que já estão na tela.

`message.isMine` não é persistido. O backend calcula o valor em cada consulta comparando `message.senderId` com o ID do usuário autenticado.

Quando não houver página anterior:

```json
{
  "data": {
    "items": [],
    "pagination": {
      "hasNext": false,
      "nextCursor": null,
      "pageSize": 50
    }
  }
}
```

### Erros

| HTTP | `error.code` | Quando ocorre |
| ---: | --- | --- |
| `400` | `invalid_pagination` | Tamanho ou cursor inválido/incompleto. |
| `401` | `unauthenticated` | Sessão ausente ou inválida. |
| `404` | `chat_not_found` | Chat inexistente ou usuário não participante. |
| `405` | `method_not_allowed` | Método diferente de `GET`. |
| `504` | `request_timeout` | A consulta excedeu o prazo. |
| `500` | `internal_error` | Falha inesperada. |

## Persistência

Os itens ficam na coleção MongoDB `timeline_items`. Um documento de mensagem usa o formato abaixo; `isMine` deliberadamente não faz parte dele:

```json
{
  "_id": "6ab85d4aa94d3539db5a83a6",
  "chat_id": "6ab85d4aa94d3539db5a83a0",
  "kind": "MESSAGE",
  "created_at": "2026-09-27T00:03:46.056Z",
  "updated_at": "2026-09-27T00:03:46.056Z",
  "message": {
    "sender_id": "6a7521f266ec07731754f78e",
    "content": {
      "type": "TEXT",
      "value": "Bom dia! Tudo bem?"
    }
  }
}
```

A leitura usa o índice `timeline_chat_created` em `chat_id`, `created_at` decrescente e `_id` decrescente.
