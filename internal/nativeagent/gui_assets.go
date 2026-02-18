package nativeagent

const guiIndexHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width, initial-scale=1"/>
  <title>Proxer Agent</title>
  <style>
    body {
      margin: 0;
      padding: 24px;
      background: #080b14;
      color: #e7edff;
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    }
    main {
      max-width: 760px;
      margin: 0 auto;
      background: #121a2b;
      border: 1px solid #2e3b56;
      border-radius: 12px;
      padding: 20px;
      line-height: 1.45;
    }
    code {
      color: #7fe0ff;
    }
  </style>
</head>
<body>
  <main>
    <h1>Proxer Agent UI Assets Missing</h1>
    <p>The embedded React UI bundle was not found in this build.</p>
    <p>Rebuild desktop UI assets:</p>
    <p><code>cd agentweb && npm install && npm run sync-native-static</code></p>
    <p>Then rebuild the agent binary.</p>
  </main>
</body>
</html>
`
