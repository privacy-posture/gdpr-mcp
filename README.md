# GDPR MCP Server

A local Model Context Protocol (MCP) server for searching GDPR documents using hybrid trigram and vector search.

## Table of Contents

- [Prerequisites](#prerequisites)
- [Quick Start](#quick-start)
- [Integration Guides](#integration-guides)
  - [Claude Desktop](#claude-desktop-setup)
  - [Ollama](#ollama-setup)
- [CLI Reference](#cli-commands)
- [Environment Variables](#environment-variables)
- [Troubleshooting](#troubleshooting)

## Prerequisites

- **Go 1.21+** - [Download Go](https://golang.org/dl/)
- **GCC** - Required for SQLite (CGO)
  - macOS: `xcode-select --install`
  - Ubuntu/Debian: `sudo apt install build-essential`
  - Fedora: `sudo dnf install gcc`

## Quick Start

### Step 1: Clone and Build

```bash
# Clone the repository
git clone https://github.com/jc/gdpr-mcp.git
cd gdpr-mcp

# Build the binary
go build -o gdpr-mcp ./cmd/gdpr-mcp

# (Optional) Install to your PATH
sudo cp gdpr-mcp /usr/local/bin/
```

### Step 2: Get GDPR Text

Download the full GDPR regulation text:

```bash
# Option A: Download from EUR-Lex (official source)
curl -o gdpr.txt "https://eur-lex.europa.eu/legal-content/EN/TXT/?uri=CELEX:32016R0679"

# Option B: Create a sample file for testing
cat > gdpr.txt << 'EOF'
Article 15 - Right of access by the data subject

1. The data subject shall have the right to obtain from the controller confirmation as to whether or not personal data concerning him or her are being processed, and, where that is the case, access to the personal data and the following information:

(a) the purposes of the processing;
(b) the categories of personal data concerned;
(c) the recipients or categories of recipient to whom the personal data have been or will be disclosed.

Article 17 - Right to erasure ('right to be forgotten')

1. The data subject shall have the right to obtain from the controller the erasure of personal data concerning him or her without undue delay and the controller shall have the obligation to erase personal data without undue delay.

Article 20 - Right to data portability

1. The data subject shall have the right to receive the personal data concerning him or her, which he or she has provided to a controller, in a structured, commonly used and machine-readable format.
EOF
```

### Step 3: Ingest the GDPR Text

```bash
./gdpr-mcp ingest gdpr.txt
```

Expected output:
```
Database path: /Users/you/.local/share/gdpr-mcp/gdpr.db
Input file: gdpr.txt
Ingesting 3 chunks...
Successfully ingested 3 chunks
Ingestion complete!
```

### Step 4: Verify Setup

```bash
./gdpr-mcp status
```

Expected output:
```
Server is not running
Database: /Users/you/.local/share/gdpr-mcp/gdpr.db
Chunks: 3
Ingested at: 2024-01-15T10:30:00Z
```

### Step 5: Test the Server

```bash
./gdpr-mcp start
```

You should see:
```
╔════════════════════════════════════════════════════════════╗
║              GDPR MCP Server Started                       ║
╠════════════════════════════════════════════════════════════╣
║  Transport:  stdio (stdin/stdout JSON-RPC 2.0)             ║
║  PID:        12345                                         ║
║  Database:   /Users/you/.local/share/gdpr-mcp/gdpr.db      ║
║  Embeddings: Local (stub)                                  ║
╠════════════════════════════════════════════════════════════╣
║  Ready to accept MCP client connections                    ║
║  Press Ctrl+C to stop                                      ║
╚════════════════════════════════════════════════════════════╝
```

Press `Ctrl+C` to stop.

---

## Integration Guides

### Claude Desktop Setup

Claude Desktop has native MCP support. Follow these steps to integrate the GDPR server.

#### Step 1: Locate the Config File

| Platform | Config Location |
|----------|-----------------|
| macOS | `~/Library/Application Support/Claude/claude_desktop_config.json` |
| Windows | `%APPDATA%\Claude\claude_desktop_config.json` |
| Linux | `~/.config/Claude/claude_desktop_config.json` |

#### Step 2: Edit the Config

Create or edit the config file:

```bash
# macOS
mkdir -p ~/Library/Application\ Support/Claude
nano ~/Library/Application\ Support/Claude/claude_desktop_config.json
```

Add this configuration (adjust the path to match your installation):

```json
{
  "mcpServers": {
    "gdpr": {
      "command": "/usr/local/bin/gdpr-mcp",
      "args": ["start"],
      "env": {}
    }
  }
}
```

**Path options:**
- If installed globally: `"/usr/local/bin/gdpr-mcp"`
- If in project directory: `"/full/path/to/gdpr-mcp/gdpr-mcp"`
- On Windows: `"C:\\path\\to\\gdpr-mcp.exe"`

#### Step 3: Restart Claude Desktop

1. Quit Claude Desktop completely (Cmd+Q on macOS, not just close window)
2. Reopen Claude Desktop
3. The GDPR tools should appear in Claude's tool list

#### Step 4: Test the Integration

In Claude Desktop, try:
> "Search the GDPR for information about the right to erasure"

Claude will use the `gdpr_search` tool and return relevant GDPR sections.

---

### Ollama Setup

Ollama doesn't have native MCP support, but you can integrate this server using **Open WebUI** or a **custom bridge script**.

#### Option A: Using Open WebUI (Recommended)

[Open WebUI](https://github.com/open-webui/open-webui) is a feature-rich web interface for Ollama with MCP support.

##### Step 1: Install Open WebUI

```bash
# Using Docker (recommended)
docker run -d -p 3000:8080 \
  --add-host=host.docker.internal:host-gateway \
  -v open-webui:/app/backend/data \
  --name open-webui \
  --restart always \
  ghcr.io/open-webui/open-webui:main

# Or using pip
pip install open-webui
open-webui serve
```

##### Step 2: Configure MCP in Open WebUI

1. Open http://localhost:3000 in your browser
2. Go to **Settings** → **Tools** → **MCP Servers**
3. Add a new MCP server:
   - **Name**: `gdpr`
   - **Command**: `/usr/local/bin/gdpr-mcp`
   - **Arguments**: `start`
4. Save and restart Open WebUI

##### Step 3: Use with Ollama Models

1. Select an Ollama model (e.g., `llama3.2`, `mistral`)
2. The GDPR tools will be available to the model
3. Ask: "Search GDPR for data portability rights"

---

#### Option B: Using MCP CLI for Testing

You can test the MCP server directly without a UI:

##### Step 1: Install mcp-cli

```bash
npm install -g @anthropic/mcp-cli
```

##### Step 2: Create an MCP Config

Create `~/.mcp/config.json`:

```json
{
  "servers": {
    "gdpr": {
      "command": "/usr/local/bin/gdpr-mcp",
      "args": ["start"]
    }
  }
}
```

##### Step 3: Test the Server

```bash
# List available tools
mcp-cli tools list --server gdpr

# Call a tool
mcp-cli tools call --server gdpr --tool gdpr_search --args '{"query": "right of access"}'
```

---

#### Option C: Custom Python Bridge

For programmatic access with Ollama, create a bridge script:

```python
#!/usr/bin/env python3
"""Bridge between Ollama and GDPR MCP server."""

import json
import subprocess
import sys

def call_mcp_tool(tool_name: str, arguments: dict) -> str:
    """Call an MCP tool and return the result."""
    # Start the MCP server
    proc = subprocess.Popen(
        ["/usr/local/bin/gdpr-mcp", "start"],
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True
    )

    # Initialize
    init_req = {
        "jsonrpc": "2.0",
        "id": 1,
        "method": "initialize",
        "params": {
            "protocolVersion": "2024-11-05",
            "capabilities": {},
            "clientInfo": {"name": "python-bridge", "version": "1.0"}
        }
    }
    proc.stdin.write(json.dumps(init_req) + "\n")
    proc.stdin.flush()
    proc.stdout.readline()  # Read init response

    # Send initialized notification
    proc.stdin.write(json.dumps({"jsonrpc": "2.0", "method": "initialized"}) + "\n")
    proc.stdin.flush()

    # Call the tool
    tool_req = {
        "jsonrpc": "2.0",
        "id": 2,
        "method": "tools/call",
        "params": {
            "name": tool_name,
            "arguments": arguments
        }
    }
    proc.stdin.write(json.dumps(tool_req) + "\n")
    proc.stdin.flush()

    # Read response
    response = json.loads(proc.stdout.readline())
    proc.terminate()

    if "result" in response:
        return response["result"]["content"][0]["text"]
    return json.dumps(response.get("error", {}))


def search_gdpr(query: str, limit: int = 5) -> str:
    """Search GDPR documents."""
    return call_mcp_tool("gdpr_search", {"query": query, "limit": limit})


def get_gdpr_chunk(chunk_id: int) -> str:
    """Get a specific GDPR chunk by ID."""
    return call_mcp_tool("gdpr_get", {"id": chunk_id})


if __name__ == "__main__":
    # Example usage
    results = search_gdpr("right to be forgotten")
    print(results)
```

Use this bridge with Ollama's Python library:

```python
import ollama
from gdpr_bridge import search_gdpr

# Search GDPR first
gdpr_context = search_gdpr("data portability")

# Use context with Ollama
response = ollama.chat(
    model="llama3.2",
    messages=[{
        "role": "user",
        "content": f"Based on this GDPR context:\n{gdpr_context}\n\nExplain data portability rights."
    }]
)
print(response["message"]["content"])
```

---

## CLI Commands

| Command | Description |
|---------|-------------|
| `gdpr-mcp ingest <file>` | Import GDPR text into the database |
| `gdpr-mcp start` | Start the MCP server (stdio mode) |
| `gdpr-mcp stop` | Stop a running server |
| `gdpr-mcp status` | Check server and database status |
| `gdpr-mcp version` | Show version |
| `gdpr-mcp help` | Show help |

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `GDPR_MCP_DB` | Custom database path | `~/.local/share/gdpr-mcp/gdpr.db` |
| `OPENAI_API_KEY` | OpenAI API key for better embeddings | _(none)_ |
| `GDPR_MCP_OPENAI` | Set to `1` to enable OpenAI | _(disabled)_ |

## Using OpenAI Embeddings (Optional)

For better semantic search, use OpenAI embeddings instead of the local stub:

```bash
# Set your API key
export OPENAI_API_KEY="sk-..."
export GDPR_MCP_OPENAI=1

# Re-ingest with real embeddings
./gdpr-mcp ingest gdpr.txt
```

## MCP Tools Reference

### gdpr_search

Search GDPR documents using hybrid search (trigram + vector similarity).

**Parameters:**
- `query` (string, required): Search query
- `limit` (integer, optional): Max results (default: 10)

**Example:**
```json
{"name": "gdpr_search", "arguments": {"query": "right to be forgotten", "limit": 5}}
```

### gdpr_get

Retrieve a full document chunk by ID.

**Parameters:**
- `id` (integer, required): Document chunk ID

**Example:**
```json
{"name": "gdpr_get", "arguments": {"id": 17}}
```

## How It Works

1. **Ingestion**: GDPR text is split into ~1000 char chunks with 100 char overlap
2. **Trigram Index**: Chunks are indexed using 3-character sequences
3. **Vector Embeddings**: Chunks are converted to vectors (OpenAI or local stub)
4. **Hybrid Search**: Queries use both methods, combined with Reciprocal Rank Fusion

## Troubleshooting

### "database not found" error

Run the ingest command first:
```bash
./gdpr-mcp ingest gdpr.txt
```

### "server already running" error

Stop the existing server:
```bash
./gdpr-mcp stop
```

### CGO/SQLite build errors

Ensure GCC is installed:
```bash
# macOS
xcode-select --install

# Ubuntu/Debian
sudo apt install build-essential
```

### Claude Desktop doesn't see the tools

1. Verify the config file path is correct
2. Use an absolute path to the binary
3. Quit and restart Claude Desktop (Cmd+Q / Alt+F4)
4. Check logs: `~/Library/Logs/Claude/` (macOS)

### Open WebUI can't connect to MCP

1. Ensure gdpr-mcp is in PATH or use absolute path
2. Check that the database exists (`./gdpr-mcp status`)
3. Restart Open WebUI after config changes

## Running Tests

```bash
go test ./... -v
```

## Project Structure

```
gdpr-mcp/
├── cmd/gdpr-mcp/main.go      # CLI entry point
├── internal/
│   ├── db/                   # Database layer
│   ├── ingest/               # Text processing
│   └── server/               # MCP server
├── go.mod
└── README.md
```

