# Contrato HTTP — Chats

Este documento descreve o contrato público que o frontend deve usar para criar e listar chats diretos.

## Visão geral

| Operação | Método | Rota | Autenticação |
| --- | --- | --- | --- |
| Criar ou obter chat direto | `POST` | `/v1/chats/direct` | Obrigatória |
| Listar chats do usuário | `GET` | `/v1/chats` | Obrigatória |
| Listar timeline do chat | `GET` | `/v1/chats/:chatId/timeline` | Obrigatória |

As duas rotas usam o access token armazenado em cookie `HttpOnly`. O frontend não deve ler nem enviar o JWT manualmente; deve permitir que o navegador envie os cookies com `credentials: "include"` ou `withCredentials: true`.

As respostas com corpo usam:

```http
Content-Type: application/json; charset=utf-8
Cache-Control: no-store
```

## Modelo de chat

```json
{
  "id": "21696fca-8846-4e0c-9479-57f079e2290c",
  "relatedUser": "66db68b1-a3e9-4c24-9ed1-8069c742e7aa",
  "relatedUserEmail": "bia@example.com",
  "createdAt": "2026-09-20T18:30:00Z",
  "updatedAt": "2026-09-20T18:30:00Z",
  "summarySortAt": "2026-09-20T18:30:00Z"
}
```

| Campo | Tipo | Descrição |
| --- | --- | --- |
| `id` | string | Identificador único do chat. |
| `relatedUser` | string | ID do outro participante em relação ao usuário autenticado. Não é um objeto de usuário. |
| `relatedUserEmail` | string | E-mail do outro participante na listagem; vazio se o cadastro não existir. |
| `createdAt` | string | Data de criação em UTC, no formato RFC 3339. |
| `updatedAt` | string | Data da última atualização em UTC, no formato RFC 3339. |
| `summarySortAt` | string | Data usada para ordenar a lista de chats, em ordem decrescente. |

Um chat direto é compartilhado pelos dois participantes. O backend garante que exista no máximo um chat direto para o mesmo par de usuários, independentemente de quem iniciou a conversa.

`relatedUserEmail` é incluído na resposta de `GET /v1/chats`; a resposta de criação continua contendo `relatedUser` sem esse campo adicional.

---

## Criar ou obter um chat direto

```http
POST /v1/chats/direct
Content-Type: application/json
```

### Corpo da requisição

```json
{
  "relatedUserId": "66db68b1-a3e9-4c24-9ed1-8069c742e7aa"
}
```

| Campo | Tipo | Obrigatório | Descrição |
| --- | --- | --- | --- |
| `relatedUserId` | string | Sim | ID do usuário com quem o usuário autenticado deseja conversar. |

O ID do usuário autenticado nunca deve ser enviado no corpo. Ele é obtido pelo backend a partir da sessão autenticada.

Campos desconhecidos no JSON são rejeitados. O corpo aceita somente um valor JSON e possui limite de 4 KiB.

### Resposta — chat criado

Status: `201 Created`

```json
{
  "data": {
    "id": "21696fca-8846-4e0c-9479-57f079e2290c",
    "relatedUser": "66db68b1-a3e9-4c24-9ed1-8069c742e7aa",
    "createdAt": "2026-09-20T18:30:00Z",
    "updatedAt": "2026-09-20T18:30:00Z",
    "summarySortAt": "2026-09-20T18:30:00Z"
  }
}
```

### Resposta — chat já existente

Status: `200 OK`

O formato do corpo é idêntico ao da resposta `201`. O `id` retornado é o identificador do chat que já existia.

O frontend deve tratar `200` e `201` como sucesso e navegar usando:

```text
/chats/:chatId
```

### Exemplo com Fetch

```ts
async function openDirectChat(relatedUserId: string): Promise<Chat> {
  const response = await fetch(`${API_URL}/v1/chats/direct`, {
    method: "POST",
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ relatedUserId }),
  });

  const payload: CreateDirectChatResponse | APIErrorResponse =
    await response.json();

  if (!response.ok) {
    throw new APIError(payload as APIErrorResponse, response.status);
  }

  return (payload as CreateDirectChatResponse).data;
}

const chat = await openDirectChat(userId);
navigate(`/chats/${chat.id}`);
```

### Exemplo com Axios

```ts
const response = await api.post<CreateDirectChatResponse>(
  "/v1/chats/direct",
  { relatedUserId: userId },
);

navigate(`/chats/${response.data.data.id}`);
```

### Erros da criação

| HTTP | `error.code` | Quando ocorre |
| ---: | --- | --- |
| `400` | `invalid_request` | Corpo vazio, JSON malformado, mais de um valor JSON ou campo desconhecido. |
| `401` | `unauthenticated` | Cookie ausente, token inválido/expirado ou sessão inativa. |
| `403` | `cross_origin_request_rejected` | A proteção de origem rejeitou a requisição. |
| `404` | `related_user_not_found` | O usuário informado não existe. |
| `405` | `method_not_allowed` | Foi usado um método diferente de `POST`. A resposta inclui `Allow: POST`. |
| `413` | `request_body_too_large` | O corpo ultrapassou 4 KiB. |
| `415` | `unsupported_media_type` | O corpo não foi enviado como `application/json`. |
| `422` | `invalid_related_user` | `relatedUserId` está vazio ou é inválido. |
| `422` | `self_direct_chat_not_allowed` | O usuário tentou iniciar um chat consigo mesmo. |
| `504` | `request_timeout` | A operação excedeu o tempo limite. |
| `500` | `internal_error` | Ocorreu uma falha inesperada. |

---

## Listar chats

```http
GET /v1/chats
```

Não envie `Content-Type` nessa requisição, pois ela não possui corpo.

### Parâmetros de query

| Parâmetro | Tipo | Padrão | Regras |
| --- | --- | --- | --- |
| `page` | integer | `1` | Deve ser maior ou igual a `1`. |
| `pageSize` | integer | `30` | Deve estar entre `1` e `100`. |
| `cursorId` | string | — | Obrigatório a partir da segunda página. Deve ser enviado com `cursorSortAt`. |
| `cursorSortAt` | string | — | Data RFC 3339. Deve ser enviada com `cursorId`. |

Um parâmetro não pode aparecer mais de uma vez. Por exemplo, `?page=1&page=2` é inválido.

### Primeira página

```http
GET /v1/chats
```

Ou explicitamente:

```http
GET /v1/chats?page=1&pageSize=30
```

A primeira página não aceita cursor.

### Página seguinte

Use exatamente o cursor recebido na resposta anterior:

```http
GET /v1/chats?page=2&pageSize=30&cursorId=6aa9a2733e91f12511ae3574&cursorSortAt=2026-09-15T23%3A46%3A40.213Z
```

O frontend deve codificar os valores com `URLSearchParams` ou `encodeURIComponent`.

### Resposta — `200 OK`

```json
{
  "data": [
    {
      "id": "21696fca-8846-4e0c-9479-57f079e2290c",
      "relatedUser": "66db68b1-a3e9-4c24-9ed1-8069c742e7aa",
      "createdAt": "2026-09-20T18:30:00Z",
      "updatedAt": "2026-09-20T18:30:00Z",
      "summarySortAt": "2026-09-20T18:30:00Z"
    }
  ],
  "pagination": {
    "hasNext": true,
    "nextCursor": {
      "id": "21696fca-8846-4e0c-9479-57f079e2290c",
      "sortAt": "2026-09-20T18:30:00Z"
    },
    "page": 1,
    "pageSize": 30,
    "totalItems": 45,
    "totalPages": 2
  }
}
```

| Campo | Tipo | Descrição |
| --- | --- | --- |
| `data` | array | Chats do usuário autenticado. Pode ser um array vazio. |
| `pagination.hasNext` | boolean | Indica se há outra página. |
| `pagination.nextCursor` | object ou `null` | Cursor para a próxima página. É `null` quando `hasNext` é `false`. |
| `pagination.nextCursor.id` | string | ID do último chat retornado. |
| `pagination.nextCursor.sortAt` | string | `summarySortAt` do último chat retornado. |
| `pagination.page` | integer | Página informada pelo cliente ou o padrão `1`. |
| `pagination.pageSize` | integer | Quantidade máxima de itens solicitada. |
| `pagination.totalItems` | integer | Total atual de chats do usuário. |
| `pagination.totalPages` | integer | Total calculado a partir de `totalItems` e `pageSize`. |

Quando não houver chats:

```json
{
  "data": [],
  "pagination": {
    "hasNext": false,
    "nextCursor": null,
    "page": 1,
    "pageSize": 30,
    "totalItems": 0,
    "totalPages": 0
  }
}
```

### Ordenação

Os chats são ordenados por:

1. `summarySortAt` decrescente;
2. `id` decrescente como desempate.

Não ordene novamente no frontend se a intenção for preservar a ordem definida pelo servidor.

### Exemplo de paginação com Axios

```ts
async function listChats(
  page = 1,
  pageSize = 30,
  cursor?: ChatCursor,
): Promise<ListChatsResponse> {
  const response = await api.get<ListChatsResponse>("/v1/chats", {
    params: {
      page,
      pageSize,
      cursorId: cursor?.id,
      cursorSortAt: cursor?.sortAt,
    },
  });

  return response.data;
}

const firstPage = await listChats();

if (firstPage.pagination.hasNext && firstPage.pagination.nextCursor) {
  const secondPage = await listChats(
    firstPage.pagination.page + 1,
    firstPage.pagination.pageSize,
    firstPage.pagination.nextCursor,
  );
}
```

### Erros da listagem

| HTTP | `error.code` | Quando ocorre |
| ---: | --- | --- |
| `400` | `invalid_pagination` | Página, tamanho ou cursor inválido/inconsistente. |
| `401` | `unauthenticated` | Cookie ausente, token inválido/expirado ou sessão inativa. |
| `403` | `cross_origin_request_rejected` | A proteção de origem rejeitou a requisição. |
| `405` | `method_not_allowed` | Foi usado um método diferente de `GET`. A resposta inclui `Allow: GET`. |
| `504` | `request_timeout` | A operação excedeu o tempo limite. |
| `500` | `internal_error` | Ocorreu uma falha inesperada. |

---

## Envelope de erro

Todas as respostas de erro usam:

```json
{
  "error": {
    "code": "invalid_pagination",
    "message": "Os parâmetros de paginação são inválidos."
  }
}
```

O frontend deve usar `error.code` para decisões programáticas. `error.message` é destinado à exibição e pode mudar sem alterar a semântica do erro.

## Tipos TypeScript sugeridos

```ts
export interface Chat {
  id: string;
  relatedUser: string;
  createdAt: string;
  updatedAt: string;
  summarySortAt: string;
}

export interface ChatCursor {
  id: string;
  sortAt: string;
}

export interface ChatPagination {
  hasNext: boolean;
  nextCursor: ChatCursor | null;
  page: number;
  pageSize: number;
  totalItems: number;
  totalPages: number;
}

export interface CreateDirectChatRequest {
  relatedUserId: string;
}

export interface CreateDirectChatResponse {
  data: Chat;
}

export interface ListChatsResponse {
  data: Chat[];
  pagination: ChatPagination;
}

export interface APIErrorResponse {
  error: {
    code: string;
    message: string;
  };
}
```

## Configuração recomendada do Axios

```ts
import axios from "axios";

export const api = axios.create({
  baseURL: import.meta.env.VITE_API_URL,
  withCredentials: true,
});
```

Ao receber `401 unauthenticated`:

1. Chame `POST /v1/auth/refresh` com credenciais.
2. Se o refresh retornar `204`, repita a requisição original uma única vez.
3. Se o refresh retornar `401`, limpe o estado local e redirecione para o login.
4. Evite múltiplos refreshes simultâneos usando uma promessa compartilhada ou fila no cliente HTTP.

## Consistência e concorrência

- Repetir `POST /v1/chats/direct` para o mesmo par devolve o mesmo chat.
- Requisições concorrentes para o mesmo par não devem criar documentos duplicados.
- `totalItems` representa uma contagem feita no momento da consulta e pode mudar entre páginas.
- Como a paginação é baseada em atividade, um chat atualizado entre duas requisições pode mudar de posição.

## Fora do escopo atual

Este contrato ainda não define:

- `GET /v1/chats/:chatId` para carregar um chat individual;
- endpoints de mensagens;
- envio, edição ou exclusão de mensagens;
- atualizações em tempo real.

O frontend já pode criar um chat, listar chats e navegar para `/chats/:chatId`, mas o carregamento da tela individual e da timeline dependerá desses endpoints futuros.
