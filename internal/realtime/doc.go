// Package realtime relays and stores Yjs updates for live editing over
// WebSocket (/rt), speaking the y-websocket protocol so the standard
// client works.
//
// The server does not merge Yjs updates itself: it keeps the update log
// of each document and lets a connected editor send a merged snapshot
// when the log grows (compaction). Markdown stays the document content;
// editors save it while they type. Changes from outside live editing
// (API, MCP, restore) reset the room so all editors reload.
package realtime
