# Contrato HTTP — Usuários

Este documento descreve o contrato público que o frontend deve usar para consumir a listagem de usuários.

## Visão geral

- Método: `GET`
- Rota: `/v1/users`
- Corpo da requisição: nenhum
- Autenticação: cookie de access token `HttpOnly`
- Resposta com corpo: `application/json; charset=utf-8`
- Sucesso: `200 OK`

A rota é autenticada. O frontend não deve ler nem enviar o JWT manualmente; deve permitir que o navegador envie o cookie usando `credentials: "include"` ou `withCredentials: true`.

Atualmente, qualquer usuário autenticado pode consultar a lista. Não existe autorização por papel administrativo.

## Requisição com `fetch`

```ts
const response = await fetch(`${API_URL}/v1/users`, {
  method: "GET",
  credentials: "include",
});

const payload: ListUsersResponse | APIErrorResponse = await response.json();
```

Não envie `Content-Type` em uma requisição sem corpo. Isso evita um preflight CORS desnecessário.

## Requisição com Axios

```ts
const api = axios.create({
  baseURL: API_URL,
  withCredentials: true,
});

const response = await api.get<ListUsersResponse>("/v1/users");
```

## Resposta de sucesso — `200 OK`

```json
{
  "data": [
    {
      "user_id": "0fc4810e-0307-4666-9687-679c8cca499a",
      "email": "ana@example.com",
      "created_at": "2026-09-03T18:30:00Z"
    },
    {
      "user_id": "66db68b1-a3e9-4c24-9ed1-8069c742e7aa",
      "email": "bia@example.com",
      "created_at": "2026-09-04T13:15:00Z"
    }
  ]
}
```

| Campo | Tipo | Descrição |
| --- | --- | --- |
| `data` | array | Lista de usuários. É um array vazio quando não existem cadastros. |
| `data[].user_id` | string | Identificador único do usuário. |
| `data[].email` | string | Endereço de e-mail normalizado. |
| `data[].created_at` | string | Data de criação em UTC, no formato RFC 3339. |

Os registros são ordenados por `created_at` do mais antigo para o mais recente. O `user_id` é usado como desempate.

Quando não houver usuários, a resposta será:

```json
{
  "data": []
}
```

O hash da senha e os tokens de autenticação nunca fazem parte da resposta.

## Tipos TypeScript sugeridos

```ts
export interface ListedUser {
  user_id: string;
  email: string;
  created_at: string;
}

export interface ListUsersResponse {
  data: ListedUser[];
}

export interface APIErrorResponse {
  error: {
    code: string;
    message: string;
  };
}
```

## Respostas de erro

Todas as respostas de erro possuem este envelope:

```json
{
  "error": {
    "code": "unauthenticated",
    "message": "Faça login para continuar."
  }
}
```

| HTTP | `error.code` | Quando ocorre | Ação recomendada no frontend |
| --- | --- | --- | --- |
| `401` | `unauthenticated` | Cookie ausente, access token inválido ou expirado, ou sessão inativa. | Tentar renovar a sessão uma vez; se falhar, direcionar ao login. |
| `403` | `cross_origin_request_rejected` | A proteção de origem rejeitou a requisição. | Verificar a origem configurada e não repetir automaticamente. |
| `405` | `method_not_allowed` | Foi usado um método diferente de `GET`. | Corrigir o método. A resposta inclui `Allow: GET`. |
| `504` | `request_timeout` | A autenticação ou a consulta excedeu o prazo. | Permitir nova tentativa. |
| `500` | `internal_error` | Falha inesperada no servidor. | Exibir mensagem genérica e permitir nova tentativa. |

## Tratamento recomendado de `401`

1. Ao receber `401 unauthenticated`, chamar `POST /v1/auth/refresh` com credenciais.
2. Se o refresh responder `204`, repetir `GET /v1/users` uma única vez.
3. Se o refresh responder `401`, limpar o estado local e direcionar ao login.
4. Não iniciar vários refreshes simultâneos; use uma fila ou uma promessa compartilhada no cliente HTTP.

## CORS

A origem do frontend deve estar presente em `HTTP_ALLOWED_ORIGINS`. Para chamadas entre origens diferentes, a API responde com credenciais habilitadas e permite os métodos `GET` e `POST`:

```http
Access-Control-Allow-Origin: http://localhost:5173
Access-Control-Allow-Credentials: true
Access-Control-Allow-Methods: GET, POST
Access-Control-Allow-Headers: Content-Type
```

O valor de `Access-Control-Allow-Origin` varia conforme a origem permitida que realizou a requisição.
