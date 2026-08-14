# ProjectView — Gestão de Projetos

Aplicação web de gestão de projetos, equipes e tarefas, com quadro kanban
(cards arrastáveis), alocação de recursos, alertas individuais de prazo,
chat interno, gráficos de acompanhamento e login integrado ao Active
Directory. 100% containerizado.

**Stack:** backend em **Go**, frontend em **React + TypeScript**, MongoDB.

## Funcionalidades

- **Login integrado ao AD**: autenticação via LDAP/Active Directory
  (`AD_ENABLED=true`), com fallback para contas locais (usuário/senha com
  bcrypt) quando o AD não está configurado.
- **Equipes, projetos, tarefas e sub-tarefas**: CRUD completo. Sub-tarefas
  são apenas tarefas com `parentTask` apontando para a tarefa pai, permitindo
  aninhamento.
- **Alocação de recursos**: qualquer pessoa pode ser atribuída a várias
  tarefas e projetos ao mesmo tempo. A tela "Alocação de Recursos" mostra a
  carga de trabalho (tarefas abertas, horas estimadas, projetos, atrasos)
  por pessoa.
- **Datas de início/fim por tarefa**, com indicação visual de tarefas
  atrasadas.
- **Alertas individuais de prazo**: um job agendado (cron, configurável)
  varre as tarefas e notifica cada responsável — em tempo real (WebSocket)
  e por e-mail — quando uma tarefa está próxima do prazo ou atrasada.
- **Integração com e-mail interno (SMTP)**: usada para os alertas de prazo e
  notificações de atribuição de tarefas. Totalmente configurável via `.env`.
  Se desabilitado, os e-mails são apenas logados, sem quebrar o resto do sistema.
- **Chat interno**: canais por equipe/projeto (criados automaticamente) e
  mensagens diretas (DM), com histórico salvo no MongoDB. Mensagens são
  enviadas via REST e entregues em tempo real via WebSocket.
- **Gráficos de acompanhamento**: distribuição de tarefas por status, carga
  de trabalho por recurso, tendência de conclusão (30 dias) e progresso por
  projeto.
- **Dashboard com cards arrastáveis**: o painel inicial é uma grade de cards
  (KPIs + gráficos) que podem ser reposicionados por drag-and-drop; a ordem
  escolhida é persistida por usuário no navegador. Além dele, cada projeto
  tem um quadro kanban com cards de tarefa arrastáveis entre colunas
  (`@dnd-kit`), com colunas configuráveis por projeto.
- **MongoDB configurável via variável de ambiente** (`MONGO_URI`), com
  **criação automática do schema (coleções + índices) na primeira
  execução**, além de um usuário administrador, equipe e projeto de exemplo
  criados automaticamente em um banco vazio.
- **100% em containers** via Docker Compose (backend, frontend e MongoDB).

## Arquitetura

```
.
├── backend/     API REST + WebSocket em Go (chi, mongo-driver, go-ldap, JWT, cron)
└── frontend/    SPA React + TypeScript (Vite) + Recharts + @dnd-kit
```

- **Backend (Go)**: `net/http` com o roteador [chi](https://github.com/go-chi/chi),
  [mongo-driver](https://github.com/mongodb/mongo-go-driver) para MongoDB,
  autenticação via JWT (cookie httpOnly ou Bearer token),
  [go-ldap](https://github.com/go-ldap/ldap) para AD, `net/smtp` para e-mail,
  [robfig/cron](https://github.com/robfig/cron) para os alertas de prazo, e
  um hub WebSocket próprio ([gorilla/websocket](https://github.com/gorilla/websocket))
  para chat e notificações em tempo real.
- **Frontend (React + TypeScript)**: Vite, React Router, Recharts para os
  gráficos, `@dnd-kit` para o quadro kanban, e um cliente WebSocket nativo
  (sem bibliotecas externas) para receber mensagens/notificações em tempo
  real — o envio de mensagens/ações sempre passa pela API REST.
- **Banco de dados**: MongoDB. O endereço é 100% configurável via
  `MONGO_URI` — pode apontar para o container incluso no `docker-compose.yml`
  ou para qualquer outro MongoDB (Atlas, on-prem, replica set etc). Na
  primeira execução, o backend cria automaticamente todas as coleções e
  índices necessários.

### Sobre o protocolo de tempo real

Diferente de uma implementação baseada em Socket.IO, aqui o WebSocket
(`/ws?token=<jwt>`) é **somente para o servidor empurrar eventos** ("notification"
e "chat:message") para os clientes já conectados. Toda escrita (criar tarefa,
enviar mensagem, mover card no kanban etc.) acontece via chamadas REST comuns;
o servidor então distribui o evento correspondente pelo WebSocket para quem
estiver com a aba aberta. Isso mantém o protocolo simples de implementar em
Go e fácil de testar via `curl`/Postman, sem abrir mão do tempo real.

## Como rodar (Docker)

1. (Opcional, mas recomendado) Copie o arquivo de variáveis de ambiente e
   ajuste conforme necessário:

   ```bash
   cp .env.example .env
   ```

   O `docker-compose.yml` tem defaults funcionais para todas as variáveis, então
   `docker compose up` funciona mesmo sem o `.env`. Para qualquer uso real,
   defina `JWT_SECRET` com um valor aleatório. Para usar o AD, defina
   `AD_ENABLED=true` e os campos `AD_*`. Para envio de e-mail, defina
   `SMTP_ENABLED=true` e os campos `SMTP_*`.

2. Suba os containers:

   ```bash
   docker compose up --build
   ```

3. Acesse a aplicação em `http://localhost:8080`.

   - Backend/API: `http://localhost:4000/api/health`
   - MongoDB: `mongodb://localhost:27017` (exposto para debug/inspeção)

4. Login inicial: um administrador local é criado automaticamente na
   primeira execução, com as credenciais definidas em
   `BOOTSTRAP_ADMIN_USERNAME` / `BOOTSTRAP_ADMIN_PASSWORD` (padrão:
   `admin` / `ChangeMe123!`). **Troque essa senha imediatamente em produção**
   (endpoint `POST /api/users/:id/password`).

## Login com Active Directory

Defina em `.env`:

```
AD_ENABLED=true
AD_URL=ldap://dc.suaempresa.com:389
AD_BASE_DN=dc=suaempresa,dc=com
AD_DOMAIN=suaempresa.com
# Opcional: conta de serviço para buscar o DN do usuário antes do bind
AD_BIND_DN=cn=svc-projectview,ou=service accounts,dc=suaempresa,dc=com
AD_BIND_PASSWORD=********
AD_USERNAME_ATTRIBUTE=sAMAccountName
```

No primeiro login bem-sucedido via AD, um usuário local correspondente é
criado automaticamente (provisionamento just-in-time), com papel padrão
`member`. Um administrador pode promover o usuário a `admin`/`manager`
depois.

## Variáveis de ambiente principais

Veja `.env.example` para a lista completa e comentada. Resumo:

| Variável | Descrição |
|---|---|
| `MONGO_URI` | String de conexão do MongoDB (configurável) |
| `JWT_SECRET` | Segredo usado para assinar os tokens de sessão |
| `AD_ENABLED`, `AD_URL`, `AD_BASE_DN`, `AD_DOMAIN`, `AD_BIND_DN`, `AD_BIND_PASSWORD` | Configuração do login via Active Directory |
| `SMTP_ENABLED`, `SMTP_HOST`, `SMTP_PORT`, `SMTP_USER`, `SMTP_PASSWORD`, `SMTP_FROM` | Configuração do e-mail interno |
| `ALERT_CRON`, `ALERT_WARN_DAYS_BEFORE` | Frequência e antecedência dos alertas de prazo |
| `BOOTSTRAP_ADMIN_*` | Credenciais do admin criado automaticamente na primeira execução |

## Desenvolvimento local (sem Docker)

Backend (Go 1.24+):

```bash
cd backend
cp ../.env.example .env   # ajuste MONGO_URI para um Mongo local, ex.: mongodb://localhost:27017/pm_dashboard
go run ./cmd/server
```

Frontend (Node 20+):

```bash
cd frontend
npm install
npm run dev
```

O Vite já está configurado para fazer proxy de `/api` e `/ws` para
`http://localhost:4000` (ajustável via `VITE_API_PROXY_TARGET`).

## Modelo de dados (resumo)

- **User**: conta local ou proveniente do AD, papel (`admin`/`manager`/`member`).
- **Team**: equipe, com membros e líder.
- **Project**: projeto, com colunas de status configuráveis (usadas no
  kanban) e membros.
- **Task**: tarefa ou sub-tarefa (via `parentTask`), com múltiplos
  `assignees` (alocação de recursos), datas de início/fim, prioridade,
  checklist, comentários e histórico de alertas já enviados.
- **ChatChannel / ChatMessage**: canais de equipe/projeto e DMs, com
  histórico de mensagens.
- **Notification**: notificações in-app (atribuições, prazos, comentários),
  entregues em tempo real via WebSocket.

## Segurança / próximos passos sugeridos

- Trocar `JWT_SECRET` e a senha do admin padrão antes de qualquer uso real.
- Colocar o backend atrás de HTTPS (ex.: um reverse proxy/ingress) em
  produção — os cookies de sessão são marcados `secure` quando
  `NODE_ENV=production`.
- Adicionar rate limiting no endpoint de login se exposto publicamente.
- Definir `MONGO_ROOT_USERNAME`/`MONGO_ROOT_PASSWORD` e usar uma
  `MONGO_URI` com credenciais em ambientes de produção.
