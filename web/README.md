# Proxer Web Workspace

This folder contains the React + TypeScript + Vite source workspace for the next-generation frontend.

## Commands

```bash
npm install
npm run dev
npm run build
npm run sync-static
```

`sync-static` builds the SPA and copies artifacts to `internal/gateway/static/`, which is what the Go gateway serves.
