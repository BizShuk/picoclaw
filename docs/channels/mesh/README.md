# Mesh Channel: Agent-to-Agent Communication & Discovery

The Mesh channel enables PicoClaw agents to discover each other on a local network and communicate via HTTP REST and WebSocket, forming a peer-to-peer agent mesh. Each agent broadcasts MCP-like descriptors (description, capabilities) so other agents can understand what it does and delegate work accordingly.

## Architecture

```
  Agent A (LAN)                        Agent B (LAN)
 ┌──────────────────┐                 ┌──────────────────┐
 │  MeshChannel     │                 │  MeshChannel     │
 │  ├─ Discovery    │◄──UDP broadcast─►│  ├─ Discovery    │
 │  ├─ HTTP Server  │◄──POST /mesh/──►│  ├─ HTTP Server  │
 │  ├─ WebSocket    │◄──WS /mesh/ws──►│  ├─ WebSocket    │
 │  └─ Registry     │  (descriptors)  │  └─ Registry     │
 │       │          │                 │       │          │
 │  MessageBus      │                 │  MessageBus      │
 │       │          │                 │       │          │
 │  AgentLoop       │                 │  AgentLoop       │
 │  └─ delegate_to_ │                 │  └─ delegate_to_ │
 │     agent tool   │                 │     agent tool   │
 └──────────────────┘                 └──────────────────┘
```

### How it works

1. **Discovery**: Each agent periodically broadcasts a UDP announce packet on the LAN (default port `9101`, every 60 seconds). The packet carries the agent's descriptor -- its name, description, and capabilities. Other agents listening on the same port receive the packet, register the sender as a known peer in their **Remote Agent Registry**, and store the full descriptor. Peers that stop announcing are automatically evicted after a configurable TTL.

2. **Remote Agent Registry**: Discovered agents are stored with their full descriptors (description, capabilities, host, port). The registry supports lookup by agent ID and filtering by capability, enabling capability-based routing.

3. **Delegation**: The LLM can use the `delegate_to_agent` tool during its reasoning loop. Calling it without arguments lists all discovered agents and their descriptors. Calling it with an `agent_id` and `message` sends the message to that agent via WebSocket (preferred) or HTTP fallback.

4. **Communication**: Once peers are discovered, agents communicate through the shared PicoClaw HTTP server:
   - **WebSocket** (`/mesh/ws`) for persistent, bidirectional real-time messaging (preferred)
   - **HTTP REST** (`POST /mesh/message`) for request/response messaging (fallback)

5. **Integration**: The mesh channel plugs into PicoClaw's standard MessageBus. Messages from remote agents appear as inbound messages processed by the AgentLoop, just like messages from Telegram, Discord, or any other channel.

## Configuration

Add the following to your `config.json`:

```json
{
  "channels": {
    "mesh": {
      "enabled": true,
      "token": "my-shared-secret",
      "agent_id": "agent-1",
      "agent_name": "Kitchen Agent",
      "description": "Manages smart home devices in the kitchen including lights, oven, and temperature sensors",
      "capabilities": ["smart-home", "kitchen", "temperature-control", "cooking"],
      "host": "",
      "port": 9100,
      "broadcast_port": 9101,
      "broadcast_interval": 60,
      "peer_ttl": 180,
      "max_connections": 100,
      "allow_origins": ["*"],
      "allow_from": []
    }
  }
}
```

### Configuration Fields

| Field                | Type     | Default  | Description                                                                        |
| -------------------- | -------- | -------- | ---------------------------------------------------------------------------------- |
| `enabled`            | bool     | `false`  | Enable the mesh channel                                                            |
| `token`              | string   | required | Shared secret for authentication between agents                                    |
| `agent_id`           | string   | auto     | Unique identifier for this agent. Auto-generated if empty                          |
| `agent_name`         | string   | `""`     | Human-readable name for this agent                                                 |
| `description`        | string   | `""`     | Free-text description of what this agent does (broadcast to peers)                 |
| `capabilities`       | []string | `[]`     | List of capability tags describing what this agent can do                           |
| `host`               | string   | auto     | IP address to advertise. Auto-detected from the default network interface if empty |
| `port`               | int      | `9100`   | Port for the HTTP/WebSocket server (uses the shared PicoClaw gateway port)         |
| `broadcast_port`     | int      | `9101`   | UDP port for discovery broadcast packets                                           |
| `broadcast_interval` | int      | `60`     | Seconds between broadcast announcements                                            |
| `peer_ttl`           | int      | `180`    | Seconds before an unresponsive peer is evicted (default: 3x broadcast_interval)    |
| `max_connections`    | int      | `100`    | Maximum concurrent WebSocket connections                                           |
| `allow_origins`      | []string | `[]`     | Allowed CORS origins for WebSocket. Empty allows all                               |
| `allow_from`         | []string | `[]`     | Allow-list for sender IDs. Empty allows all                                        |

Environment variables follow the pattern `PICOCLAW_CHANNELS_MESH_<FIELD>`.

The `description` and `capabilities` fields can also be set on the agent config (`agents.list[].description`, `agents.list[].capabilities`) for use across all channels.

## Multi-Agent Setup

To run two agents on the same LAN that discover each other and delegate tasks:

**Agent 1** -- Living Room (`config-agent1.json`):

```json
{
  "channels": {
    "mesh": {
      "enabled": true,
      "token": "secret123",
      "agent_id": "agent-1",
      "agent_name": "Living Room",
      "description": "Controls living room entertainment system and ambient lighting",
      "capabilities": ["entertainment", "lighting", "music"]
    }
  },
  "gateway": { "port": 8080 }
}
```

**Agent 2** -- Kitchen (`config-agent2.json`):

```json
{
  "channels": {
    "mesh": {
      "enabled": true,
      "token": "secret123",
      "agent_id": "agent-2",
      "agent_name": "Kitchen",
      "description": "Manages kitchen appliances and cooking timers",
      "capabilities": ["kitchen", "cooking", "appliances"]
    }
  },
  "gateway": { "port": 8081 }
}
```

Both agents must use the **same `token`** to trust each other. They will automatically discover each other via UDP broadcast within `broadcast_interval` seconds. Once discovered, the LLM on either agent can use `delegate_to_agent` to send tasks to the other.

## The `delegate_to_agent` Tool

When the mesh channel is enabled, a `delegate_to_agent` tool is automatically registered with all agents. The LLM uses this tool during its reasoning loop to discover and communicate with remote agents.

### List available agents

Call without `agent_id` to see all discovered agents:

```json
{ "name": "delegate_to_agent", "arguments": {} }
```

Returns a list of discovered agents with their descriptions and capabilities:

```json
[
  {
    "agent_id": "agent-2",
    "name": "Kitchen",
    "description": "Manages kitchen appliances and cooking timers",
    "capabilities": ["kitchen", "cooking", "appliances"],
    "host": "192.168.1.42",
    "port": 8081
  }
]
```

### Send a message to a remote agent

```json
{
  "name": "delegate_to_agent",
  "arguments": {
    "agent_id": "agent-2",
    "message": "Set a 10-minute timer for the pasta"
  }
}
```

The tool prefers WebSocket (establishing an outbound connection if needed) and falls back to HTTP.

## HTTP API

All endpoints are mounted under `/mesh/` on the shared PicoClaw HTTP server (the gateway port).

### `POST /mesh/message`

Send a message to this agent from another agent.

**Headers:**

- `Authorization: Bearer <token>`
- `Content-Type: application/json`

**Request body:**

```json
{
  "type": "agent.message",
  "from_agent": "agent-2",
  "payload": {
    "content": "Hello from Agent 2!"
  }
}
```

**Response:**

```json
{
  "status": "ok",
  "agent_id": "agent-1",
  "ack_id": "msg-uuid"
}
```

### `GET /mesh/peers`

List all discovered peers with their descriptors.

**Headers:**

- `Authorization: Bearer <token>`

**Response:**

```json
{
  "agent_id": "agent-1",
  "peers": [
    {
      "agent_id": "agent-2",
      "name": "Kitchen",
      "description": "Manages kitchen appliances and cooking timers",
      "capabilities": ["kitchen", "cooking", "appliances"],
      "host": "192.168.1.42",
      "port": 8081,
      "features": ["http", "websocket"],
      "last_seen": "2026-02-28T12:00:00Z"
    }
  ]
}
```

### `GET /mesh/info`

Get metadata about this agent (no authentication required).

**Response:**

```json
{
  "agent_id": "agent-1",
  "agent_name": "Living Room",
  "description": "Controls living room entertainment system and ambient lighting",
  "capabilities": ["entertainment", "lighting", "music"],
  "features": ["http", "websocket", "discovery"],
  "connections": 1,
  "peers": 1
}
```

## WebSocket Protocol

Connect to `/mesh/ws?token=<token>&agent_id=<your_agent_id>` to establish a persistent bidirectional connection.

### Message Types

| Type               | Direction         | Description                           |
| ------------------ | ----------------- | ------------------------------------- |
| `agent.message`    | both              | Send/receive a message between agents |
| `agent.ack`        | server -> client  | Acknowledgment of a received message  |
| `peer.query`       | client -> server  | Request the list of known peers       |
| `peer.list`        | server -> client  | Response with peer list               |
| `ping`             | both              | Application-level ping                |
| `pong`             | both              | Application-level pong                |
| `error`            | server -> client  | Error response                        |

### Wire Format

All messages use the `MeshMessage` JSON format:

```json
{
  "type": "agent.message",
  "id": "optional-correlation-id",
  "from_agent": "agent-1",
  "to_agent": "agent-2",
  "timestamp": 1709136000000,
  "payload": {
    "content": "Hello!"
  }
}
```

### Example WebSocket Session

```
Client: {"type":"agent.message","from_agent":"agent-2","payload":{"content":"Hi there!"}}
Server: {"type":"agent.ack","from_agent":"agent-1","payload":{"ack_id":"msg-uuid"}}

Client: {"type":"peer.query","from_agent":"agent-2"}
Server: {"type":"peer.list","from_agent":"agent-1","payload":{"peers":[...]}}

Client: {"type":"ping","from_agent":"agent-2"}
Server: {"type":"pong","from_agent":"agent-1"}
```

## Discovery Protocol

Agents broadcast UDP packets on port `9101` (configurable) every `broadcast_interval` seconds (default: 60s).

### Announce Packet

```json
{
  "type": "announce",
  "agent_id": "agent-1",
  "name": "Living Room",
  "description": "Controls living room entertainment system and ambient lighting",
  "capabilities": ["entertainment", "lighting", "music"],
  "host": "192.168.1.10",
  "port": 8080,
  "features": ["http", "websocket"],
  "token": "secret123",
  "ts": 1709136000000
}
```

### Lifecycle

- **Join**: When a new `agent_id` is seen for the first time, an `OnPeerJoined` event fires and the agent is added to the Remote Agent Registry with its full descriptor.
- **Heartbeat**: Each announce resets the peer's TTL timer and updates the registry.
- **Leave**: If no announce is received within `peer_ttl` seconds, the peer is evicted from the registry and an `OnPeerLeft` event fires. Any active WebSocket connections to that peer are closed.
- **Self-filtering**: An agent ignores its own broadcast packets.
- **Token validation**: If a `token` is configured, packets with a mismatched token are silently rejected.

## Message Flow

### Inbound (receiving a message from a remote agent)

```
Remote Agent ──HTTP POST /mesh/message──► MeshChannel
                                              │
                                     processInboundAgentMessage()
                                              │
                                     BaseChannel.HandleMessage()
                                              │
                                         MessageBus
                                              │
                                         AgentLoop
                                              │
                                      LLM processes message
```

### Outbound via delegate_to_agent tool

```
User prompt ──► AgentLoop ──► LLM reasoning loop
                                      │
                             delegate_to_agent tool
                                      │
                       ┌──────────────┴──────────────┐
                  Try WebSocket                 Fall back to HTTP
              (dial if needed)             POST /mesh/message on
                                           discovered peer endpoint
```

### Outbound (sending a response to a remote agent)

```
AgentLoop ──► MessageBus ──► Manager dispatches to MeshChannel worker
                                              │
                                     Send(chatID="mesh:agent-2")
                                              │
                              ┌───────────────┴───────────────┐
                         Try WebSocket                   Fall back to HTTP
                    (connected peers)               POST /mesh/message on
                                                   discovered peer endpoint
```

## Package Structure

```
pkg/
├── discovery/
│   └── discovery.go          # UDP broadcast discovery service
├── channels/
│   └── mesh/
│       ├── init.go           # Channel factory registration
│       ├── mesh.go           # MeshChannel implementation
│       ├── protocol.go       # Wire format types and constants
│       ├── registry.go       # RemoteAgentRegistry for discovered peers
│       └── delegator.go      # MeshDelegator adapter for the delegate tool
└── tools/
    └── delegate.go           # delegate_to_agent LLM tool
```

## Security Considerations

- All HTTP and WebSocket endpoints require Bearer token authentication.
- UDP discovery packets include the shared token; mismatches are silently dropped.
- The token is transmitted in plaintext over UDP. For production use on untrusted networks, consider running agents behind a VPN or using TLS termination.
- The `allow_from` field can restrict which sender IDs are permitted.
