# Pesquisa de conversas e mensagens

As consultas de pesquisa são feitas no backend e possuem cursores próprios. Elas
não dependem das páginas carregadas pelo navegador. O MongoDB continua sendo a
fonte de verdade, sem um serviço adicional de pesquisa.

## Correspondência

- Palavras completas, sem radicais, prefixos, erros aproximados ou stop words.
- Todas as palavras precisam aparecer (AND), em qualquer ordem.
- Maiúsculas, minúsculas e acentos são normalizados, inclusive Unicode decomposto.
- Pontuação separa palavras. `ana.silva@example.com` vira `ana`, `silva`, `example`, `com`.
- `contrato` encontra `Contrato`, mas não `contratos`, `contratou` ou `contra`.
- Uma pesquisa aceita até 200 caracteres e 8 palavras distintas; pontuação sem
  palavras é inválida (`400 invalid_search`). Uma consulta vazia usa a listagem normal.
- Mensagens pesquisam texto e legendas de imagens, sem OCR. Conversas pesquisam
  somente o e-mail do outro participante, do qual o frontend deriva o nome exibido.

## Contratos HTTP

### Conversas

`GET /v1/chats?q=ana+silva&pageSize=30`

Mantém o envelope da listagem. A participação do usuário e as palavras do outro
participante são filtradas antes da paginação. A pesquisa usa ordenação imutável
`created_at DESC, _id DESC`, sem reordenação por mensagens novas. Na pesquisa,
`nextCursor.sortAt` representa `created_at`; repasse o cursor sem interpretá-lo.
`totalItems` e `totalPages` são omitidos para evitar contagens exatas. A listagem
sem `q` mantém seu contrato e ordenação por última atividade.

`GET /v1/chats/{chatId}` retorna o mesmo envelope com uma conversa, permitindo
abrir resultados e links diretos sem carregar a sidebar. Chat inexistente e chat
sem participação retornam o mesmo `404 chat_not_found`.

### Mensagens

`GET /v1/chats/{chatId}/timeline?order=sequence&q=contrato+acao&pageSize=20`

Mantém o envelope da timeline. Os itens da página são cronológicos, mas a busca
começa pelas ocorrências mais recentes. Para continuar, envie o mesmo `q`,
`order=sequence`, `nextBeforeSequence` como `beforeSequence` e o primeiro
`snapshotThroughSequence` como `throughSequence`. Mensagens posteriores ao
snapshot não entram na navegação em andamento. Não há contagem exata.

`q` não pode ser combinado com `aroundSequence`, `afterSequence` ou cursor por data.

### Contexto de uma mensagem

`GET /v1/chats/{chatId}/timeline?order=sequence&aroundSequence=845&pageSize=51`

Retorna até 25 mensagens anteriores, a mensagem 845 e até 25 posteriores. A
consulta usa diretamente `(chat_id, sequence)` e não percorre páginas intermediárias.
`pageSize` limita a janela inteira e mantém o teto de 100 itens. Uma âncora
inexistente retorna `400 invalid_pagination`. Âncoras não podem ser combinadas com
pesquisa, cursores ou outros limites de sequência.

`pagination.hasOlder` e `pagination.hasNewer` indicam conteúdo nos dois sentidos.
Para navegar, use `order=sequence&beforeSequence=<menor sequência>` ou
`afterSequence=<maior sequência>`, com `pageSize` limitado. Todas as consultas
validam a participação antes de consultar o histórico.

## Índices e gravação

`search_words` armazena palavras normalizadas e únicas na mesma transação da
mensagem. O índice `timeline_chat_word_sequence` cobre `chat_id`, palavra e
`sequence DESC`. A igualdade da primeira palavra restringe os candidatos;
palavras adicionais são filtros. A palavra mais longa é escolhida como ponto
inicial, uma heurística de seletividade, sem garantir que seja a mais rara.

Conversas guardam `participant_one_words` e `participant_two_words`. Cada
perspectiva possui um índice com o usuário, as palavras do outro participante e a
ordem de criação. Os e-mails são imutáveis na API atual. Uma futura alteração de
e-mail precisa atualizar essas projeções. Um futuro fluxo de edição de mensagens
precisa atualizar `search_words` na mesma gravação.

Os índices são preparados pelo bootstrap da API. A paginação elimina offsets,
mas não garante custo constante para termos muito frequentes ou combinações
pouco seletivas. Avalie latência, documentos examinados, tamanho dos índices e
custo de gravação com dados representativos antes de dimensionar produção.

## Histórico existente

Depois de disponibilizar a nova versão da API, execute a migração antes de liberar
a pesquisa aos usuários. Mensagens e conversas novas já recebem as palavras.
Registros antigos permanecem fora da pesquisa até serem processados.

O comando carrega o `.env` da pasta atual automaticamente, sem sobrescrever
variáveis já definidas no ambiente. `MONGODB_URI` e `MONGODB_DATABASE` são
obrigatórias. Para outro arquivo, use `-env-file caminho`; para desabilitar a
leitura, use `-env-file=""`.

```powershell
go run ./cmd/search-backfill
```

Ao executar pelo Windows com o MongoDB do Compose, a URI pode conter o hostname
interno `mongodb`. Use a porta publicada no host (`27019` no Compose padrão):

```powershell
go run ./cmd/search-backfill -mongo-address 127.0.0.1:27019
```

A opção preserva usuário, senha, banco e opções de autenticação da URI e usa
conexão direta ao endereço informado. Não é necessário expor credenciais no terminal.

A migração usa cursor em lotes, processa apenas projeções ausentes e pode ser
interrompida e retomada. Ela não roda em cada inicialização nem carrega o histórico
inteiro em memória. Não apaga mensagens, conversas ou índices existentes.

## Frontend

- `q`: pesquisa de conversas.
- `messageQuery`: pesquisa de mensagens da conversa selecionada.
- `message`: sequência da mensagem que ancora a janela histórica.

Exemplo: `/chats/chat-1?q=ana&messageQuery=contrato&message=845`.

Os campos mantêm um rascunho local durante a digitação e atualizam a URL com
`replace` somente ao pressionar Enter. Consultas usam cancelamento por
`AbortSignal` e chaves de cache por termo e conversa. Selecionar
uma mensagem cria uma entrada de navegação. Links diretos e voltar/avançar
restauram termo e âncora. A troca de conversa preserva `q` e remove a busca de
mensagens e a âncora da conversa anterior.

A janela histórica usa cache separado da timeline ao vivo e mantém até 8 páginas.
Mensagens novas sinalizam sua chegada sem preencher a lacuna até o presente ou
deslocar a leitura. Ao voltar às mais recentes, a timeline é reiniciada na cauda,
sem refazer todas as páginas antigas.

## Validação

Os testes de integração usam um replica set exclusivo indicado por
`MONGODB_TEST_URI`. Cobrem mil mensagens, palavras completas, legendas, Unicode,
snapshot da busca, paginação, janelas com âncoras antigas e nos extremos,
backfill idempotente, participantes nas duas posições e isolamento de acesso.
O teste de mensagens também verifica o plano vencedor com `explain`, rejeitando
`COLLSCAN` e ordenação bloqueante na consulta por palavra e cursor.
