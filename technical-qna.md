# Questions & Answers - Technical

1. Backend language?
My recommendation is TypeScript (Node.js) — strong ecosystem for REST APIs, GitHub/Notion
SDKs, and good TUI options. Alternatives: Python (FastAPI + Textual), Go (Bubble Tea TUI).
 Do you have a preference, or should I proceed with TypeScript?

A: Go

2. TUI framework?
This follows from the language choice. For TypeScript: Ink (React-based TUI). For Python:
Textual. For Go: Bubble Tea. Preference?

A: Bubble Tea (Go)

3. Database: SQLite or PostgreSQL?
Given single-tenant, local deployment, and zero ops requirements, SQLite is the natural
fit. Any reason to want Postgres instead?

A: SQLite

4. Deployment target?
Running locally on a Mac, in a Docker container, or on a remote server? This affects how
the TUI connects to the backend (localhost vs. network) and how .env credentials are
managed.

A: I will run locally on my Mac for now

5. GitHub & Notion auth?
- GitHub: Personal Access Token in .env?
- Notion: Integration Token in .env?
Or would you want OAuth flows for either?

A: No OAuth, tokens in .env works fine

6. AI model?
I'll use Claude API (Anthropic). Any preference on model tier? claude-sonnet-4-6 is a good
 balance of quality and cost; claude-opus-4-6 for higher reasoning tasks.
 
A: Let's make it configurable via a config.yaml file. Also, I want to be able to use my claude subscription, see ~/src/engram for one way to do that (by running claude -p)
