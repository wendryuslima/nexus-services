# Contrato HTTP — Usuários

`GET /v1/users` exige sessão autenticada por cookie `HttpOnly`. O navegador deve enviar `credentials: "include"`. A resposta é JSON com `Cache-Control: no-store`.

## Paginação

| Parâmetro | Padrão | Regra |
| --- | --- | --- |
| `page` | `1` | Inteiro positivo. Páginas após a primeira exigem cursor. |
| `pageSize` | `30` | Inteiro de 1 a 100. |
| `cursorId` | ausente | ID recebido em `pagination.nextCursor.id`. |
| `cursorCreatedAt` | ausente | Data recebida em `pagination.nextCursor.createdAt`, em RFC 3339. |

Envie `cursorId` e `cursorCreatedAt` juntos somente a partir da página 2. Parâmetros repetidos, cursor incompleto e valores inválidos recebem `400 invalid_pagination`. Os usuários são ordenados por `created_at` crescente e, em caso de empate, por `user_id` crescente. O cursor preserva essa ordem mesmo quando vários usuários têm a mesma data de criação.

```http
GET /v1/users?page=1&pageSize=1
```

```json
{
  "data": [
    {
      "user_id": "0fc4810e-0307-4666-9687-679c8cca499a",
      "email": "ana@example.com",
      "created_at": "2026-09-03T18:30:00Z"
    }
  ],
  "pagination": {
    "hasNext": true,
    "nextCursor": {
      "id": "0fc4810e-0307-4666-9687-679c8cca499a",
      "createdAt": "2026-09-03T18:30:00Z"
    },
    "page": 1,
    "pageSize": 1,
    "totalItems": 2,
    "totalPages": 2
  }
}
```

`data` é `[]` quando a página não tem registros. `nextCursor` é `null` quando `hasNext` é `false`. A senha e os tokens nunca são retornados.

```ts
type UserCursor = { id: string; createdAt: string };
type ListUsersResponse = {
  data: { user_id: string; email: string; created_at: string }[];
  pagination: {
    hasNext: boolean;
    nextCursor: UserCursor | null;
    page: number;
    pageSize: number;
    totalItems: number;
    totalPages: number;
  };
};

const params = new URLSearchParams({ page: "1", pageSize: "30" });
const response = await fetch(`${API_URL}/v1/users?${params}`, {
  method: "GET",
  credentials: "include",
});
const payload: ListUsersResponse = await response.json();
```

Para carregar mais, incremente `page` e envie os dois campos de `nextCursor` sem alterar a precisão da data. Não envie `Content-Type` em um GET sem corpo.

## Erros

Todos os erros usam `{ "error": { "code": string, "message": string } }`.

| HTTP | Código | Significado |
| --- | --- | --- |
| 400 | `invalid_pagination` | Paginação ou cursor inválido. |
| 401 | `unauthenticated` | Sessão ausente ou expirada. Tente `POST /v1/auth/refresh` uma vez e repita a requisição. |
| 403 | `cross_origin_request_rejected` | Origem rejeitada. |
| 405 | `method_not_allowed` | Método diferente de GET; `Allow: GET`. |
| 504 | `request_timeout` | Prazo de autenticação ou consulta excedido. |
| 500 | `internal_error` | Falha inesperada. |
