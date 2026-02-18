# Proxer Native Agent UI

React + TypeScript + Vite app for the native desktop agent shell.

## Local development

```bash
cd agentweb
npm install
npm run dev
```

## Build and sync to Go embed assets

```bash
cd agentweb
npm run sync-native-static
```

This command updates `../internal/nativeagent/static/`.
