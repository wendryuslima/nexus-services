# Contrato HTTP — Autenticação

Este documento descreve o contrato público da API de autenticação para o frontend.

## Visão geral

- Base path: `/v1/auth`
- Formato de entrada: `application/json`
- Formato das respostas com corpo: `application/json; charset=utf-8`
- Tokens: **não** são devolvidos no JSON. A API os armazena em cookies `HttpOnly`.
- Todas as operações de autenticação usam `POST`.

O cliente deve enviar `Content-Type: application/json` nas rotas que possuem corpo. O JSON é estrito: campos desconhecidos, corpo vazio, JSON inválido, ou mais de um valor JSON são rejeitados.

## Configuração do cliente HTTP

Como a sessão usa cookies, o cliente precisa habilitar o envio de credenciais.

```ts
// fetch
await fetch(`${API_URL}/v1/auth/signin`, {
  method: "POST",
  credentials: "include",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({ email, password }),
});
```

```ts
// axios
const api = axios.create({
  baseURL: API_URL,
  withCredentials: true,
});
```

Não leia, armazene ou envie JWTs manualmente pelo JavaScript. Os cookies são `HttpOnly` justamente para não ficarem acessíveis a scripts da página.

## Envelope de erro

Todas as respostas de erro seguem este formato:

```json
{
  "error": {
    "code": "invalid_credentials",
    "message": "E-mail ou senha incorretos."
  }
}
```

O frontend deve tomar decisões pelo campo `error.code`, não pelo texto de `error.message`.

---

## `POST /v1/auth/signup`

Cria uma conta, mas **não inicia uma sessão**. Depois de um cadastro bem-sucedido, o frontend deve direcionar a pessoa para o login ou chamar `signin` explicitamente.

### Corpo da requisição

```json
{
  "email": "ana@example.com",
  "password": "uma-senha-com-pelo-menos-12-caracteres"
}
```

| Campo | Tipo | Regras |
| --- | --- | --- |
| `email` | string | Obrigatório; normalizado para minúsculas; máximo de 254 caracteres; precisa ser um endereço de e-mail válido. |
| `password` | string | Obrigatório; entre 12 e 128 caracteres Unicode. |

O corpo total é limitado a 16 KiB.

### Resposta de sucesso — `201 Created`

```json
{
  "data": {
    "user_id": "0fc4810e-0307-4666-9687-679c8cca499a",
    "email": "ana@example.com",
    "created_at": "2026-09-03T18:30:00Z"
  }
}
```

| Código | Situação |
| --- | --- |
| `400` `invalid_request` | JSON vazio, inválido, com campos desconhecidos ou mais de um objeto JSON. |
| `413` `request_body_too_large` | Corpo acima de 16 KiB. |
| `415` `unsupported_media_type` | `Content-Type` não é `application/json`. |
| `422` `invalid_email` | E-mail inválido. |
| `422` `invalid_password` | Senha fora das regras. |
| `409` `email_already_registered` | Já existe uma conta com o e-mail informado. |
| `504` `request_timeout` | A operação excedeu o prazo do servidor. |
| `500` `internal_error` | Falha inesperada. |

---

## `POST /v1/auth/signin`

Valida credenciais, cria uma sessão e envia cookies de acesso e renovação.

### Corpo da requisição

```json
{
  "email": "ana@example.com",
  "password": "uma-senha-com-pelo-menos-12-caracteres"
}
```

### Resposta de sucesso — `200 OK`

```json
{
  "data": {
    "user_id": "0fc4810e-0307-4666-9687-679c8cca499a",
    "email": "ana@example.com"
  }
}
```

Além do corpo, a resposta inclui dois headers `Set-Cookie`:

- ambiente local (`AUTH_COOKIE_SECURE=false`): `nexus-access` e `nexus-refresh`;
- ambiente seguro (`AUTH_COOKIE_SECURE=true`): `__Host-nexus-access` e `__Host-nexus-refresh`.

Os cookies têm `HttpOnly`, `Path=/`, `Max-Age`, `Expires` e política `SameSite` definida pela API. Em ambiente seguro também possuem `Secure`.

| Código | Situação |
| --- | --- |
| `400` `invalid_request` | Corpo JSON inválido. |
| `413` `request_body_too_large` | Corpo acima de 16 KiB. |
| `415` `unsupported_media_type` | Content-Type inválido. |
| `401` `invalid_credentials` | E-mail inexistente ou senha incorreta. A API não diferencia os dois casos. |
| `504` `request_timeout` | A operação excedeu o prazo do servidor. |
| `500` `internal_error` | Falha inesperada. |

---

## `POST /v1/auth/refresh`

Renova a sessão usando o cookie de refresh. Há rotação: a cada sucesso, os dois cookies são substituídos por novos valores.

### Corpo da requisição

Não envie corpo.

### Resposta de sucesso — `204 No Content`

Não há corpo JSON. A resposta contém novos headers `Set-Cookie` para access e refresh token.

### Resposta de sessão inválida — `401 Unauthorized`

```json
{
  "error": {
    "code": "invalid_refresh_token",
    "message": "Sua sessão expirou. Entre novamente para continuar."
  }
}
```

Nesse caso, a API também expira os cookies de autenticação. O frontend deve limpar o estado local da pessoa e encaminhá-la ao login.

| Código | Situação |
| --- | --- |
| `401` `invalid_refresh_token` | Cookie ausente, duplicado, expirado, inválido, revogado ou já reutilizado. |
| `504` `request_timeout` | A operação excedeu o prazo do servidor. |
| `500` `internal_error` | Falha inesperada. |

---

## `POST /v1/auth/logout`

Revoga a sessão associada ao cookie de refresh e expira os cookies no navegador.

### Corpo da requisição

Não envie corpo.

### Resposta de sucesso — `204 No Content`

Não há corpo. Os cookies de autenticação são expirados, inclusive se o cookie de refresh já estiver ausente.

| Código | Situação |
| --- | --- |
| `204` | Logout concluído ou não havia sessão local para encerrar. |
| `504` `request_timeout` | A operação excedeu o prazo do servidor. |
| `503` `logout_unavailable` | A sessão não pôde ser revogada; tente novamente. |

---

## Métodos, rotas e CORS

| Método | Rota | Requer cookies | Sucesso |
| --- | --- | --- | --- |
| `POST` | `/v1/auth/signup` | Não | `201` |
| `POST` | `/v1/auth/signin` | Não | `200` |
| `POST` | `/v1/auth/refresh` | Cookie de refresh | `204` |
| `POST` | `/v1/auth/logout` | Cookie de refresh, quando existir | `204` |

`OPTIONS` é usado automaticamente pelo navegador para preflight CORS. Outros métodos retornam `405 method_not_allowed` e header `Allow: POST`.

Uma rota inexistente retorna:

```json
{
  "error": {
    "code": "route_not_found",
    "message": "A página solicitada não foi encontrada."
  }
}
```

Origens permitidas são definidas pelo backend em `HTTP_ALLOWED_ORIGINS` e precisam coincidir exatamente com a origem do frontend, por exemplo `http://localhost:5173`.

> **Atenção — bloqueador atual para frontend em outra origem:** o middleware CORS atual possui nomes/valores de headers incorretos. Ele sobrescreve `Access-Control-Allow-Origin` com `true`, não envia `Access-Control-Allow-Credentials: true`, e usa `Access-Control-Allow-Header` no singular. Enquanto isso não for corrigido, chamadas com `credentials: "include"` entre origens diferentes serão bloqueadas pelo navegador. Chamadas same-origin não dependem de CORS.

O contrato CORS esperado após a correção é:

```http
Access-Control-Allow-Origin: http://localhost:5173
Access-Control-Allow-Credentials: true
Access-Control-Allow-Methods: POST
Access-Control-Allow-Headers: Content-Type
```

---

## Fluxo recomendado no frontend

1. No cadastro, chame `signup`; após `201`, redirecione ao login.
2. No login, chame `signin` com `credentials: "include"`; após `200`, grave somente `user_id` e `email` no estado da interface.
3. Ao iniciar a aplicação ou receber `401` de uma rota protegida futura, chame `refresh` uma única vez.
4. Se `refresh` retornar `204`, repita a requisição original uma vez; se retornar `401`, mostre a tela de login.
5. No logout, chame `logout` com `credentials: "include"`; após `204`, limpe o estado local e redirecione ao login.

Evite chamadas concorrentes de `refresh`: como há rotação do refresh token, múltiplas chamadas simultâneas podem invalidar a sessão. Centralize a renovação em um único interceptor/fila no cliente HTTP.
