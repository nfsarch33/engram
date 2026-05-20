package engramsvc

import "strings"

const factExtractionSystem = `You are a system that extracts concise, atomic facts from conversations.
Return a JSON object with a "facts" array, each item having "text" and optional "type" fields.
Focus on persistent facts: preferences, identity, habits, relationships. Omit transient statements.`

const updateDecisionSystem = `You are a memory consolidation system.
Given existing memories and new facts, decide what to ADD, UPDATE, DELETE, or keep (NONE).
Return a JSON object with an "events" array. Each event has "event" (add/update/delete/none),
"id" (existing memory ID for update/delete, empty for add), and "text" (new or updated text).`

func buildFactExtractionPrompt(messages []string) string {
	return "Conversation:\n" + strings.Join(messages, "\n") + "\n\nExtract facts."
}

func buildUpdateDecisionPrompt(existingJSON, newFactsJSON string) string {
	return "Existing memories (JSON):\n" + existingJSON +
		"\n\nNew facts (JSON):\n" + newFactsJSON +
		"\n\nDecide which memories to add, update, delete, or leave unchanged."
}
