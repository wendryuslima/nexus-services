# Contrato HTTP — Timeline do chat

O conteúdo de uma mensagem pode ser `TEXT` (`value`) ou `IMAGE` (`imageId` e
`caption` opcional). Os exemplos de texto abaixo continuam válidos. URLs de
imagem são obtidas separadamente; veja o [contrato de imagens](images.md).

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
| `order` | string | — | `sequence` seleciona carga inicial/histórico por sequência. |
| `beforeSequence` | integer | — | Limite exclusivo de histórico, somente com `order=sequence`. |
| `afterSequence` | integer | — | Limite exclusivo incremental; aceita zero. |
| `throughSequence` | integer | — | Limite inclusivo fixo, somente com `afterSequence`. |

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
        "sequence": 183,
        "kind": "MESSAGE",
        "createdAt": "2026-09-27T00:03:46.056Z",
        "updatedAt": "2026-09-27T00:03:46.056Z",
        "message": {
          "clientMessageId": "018f92d4-2c47-7c87-b33e-d2ea331d2ff2",
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
  "sequence": 183,
  "kind": "MESSAGE",
  "created_at": "2026-09-27T00:03:46.056Z",
  "updated_at": "2026-09-27T00:03:46.056Z",
  "message": {
    "client_message_id": "018f92d4-2c47-7c87-b33e-d2ea331d2ff2",
    "sender_id": "6a7521f266ec07731754f78e",
    "content": {
      "type": "TEXT",
      "value": "Bom dia! Tudo bem?"
    }
  }
}
```

A leitura usa o índice `timeline_chat_created` em `chat_id`, `created_at` decrescente e `_id` decrescente.

## Recuperação por sequência

O modo legado acima continua disponível. Clientes realtime usam os modos abaixo, sempre com itens em sequência crescente e inteiros até `9007199254740991`.

- `?order=sequence&pageSize=50`: últimas 50 mensagens; retorna `snapshotThroughSequence` e `pagination.nextBeforeSequence`.
- `?order=sequence&beforeSequence=151&pageSize=50`: até 50 mensagens anteriores a 151; `hasNext` indica histórico anterior restante.
- `?afterSequence=182&pageSize=100`: próxima página incremental e captura um limite superior consistente.
- `?afterSequence=190&throughSequence=200&pageSize=100`: continua a recuperação até a mesma fotografia.

```json
{
  "data": {
    "items": [],
    "snapshotThroughSequence": 200,
    "pagination": {
      "hasNext": false,
      "nextCursor": null,
      "nextBeforeSequence": null,
      "nextAfterSequence": null,
      "pageSize": 100
    }
  }
}
```

O exemplo mostra apenas o envelope. `nextAfterSequence` identifica o último item da página quando existe continuação incremental; `nextBeforeSequence` identifica o primeiro item quando existe histórico anterior. Ambos ficam `null` ao terminar o respectivo modo.

Snapshot e página são lidos na mesma transação curta, com `readConcern: snapshot` e fonte primária. Cada página verifica participação na conversa. Os limites são exclusivos para `before/after` e inclusivo para `through`. Parâmetros duplicados, negativos, fora da faixa segura ou incompatíveis retornam `400 invalid_pagination`. Não combine cursores legados com os modos novos, nem `afterSequence` com `order=sequence` ou `beforeSequence`.

O frontend atual usa `order=sequence` para carregar a cauda e `beforeSequence` para carregar histórico. No fluxo normal, `message.created` atualiza diretamente o estado da timeline e da conversa conhecida, sem GET após cada POST ou evento. As mensagens do socket são deduplicadas por ID e preservadas diante de uma resposta HTTP antiga. Prontidão/reconexão, lacunas e verificações de recuperação usam releitura HTTP. Os modos `afterSequence/throughSequence` continuam disponíveis na API para outros consumidores.

As respostas da API usam `Cache-Control: no-store`, e o cliente HTTP usa `cache: "no-store"`. No TanStack Query, os dados de conversas não são retidos após ficarem sem observadores (`gcTime: 0`), são imediatamente desatualizados (`staleTime: 0`) e consultados novamente ao montar a tela. O resultado da consulta permanece em memória enquanto a tela está montada. Consulte também [operação do realtime](realtime-operations.md).
