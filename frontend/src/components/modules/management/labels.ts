export function sourceLabel(source: string): string {
  return source === "set" ? "Set" : "Default"
}

export function actorLabel(actor: { type: string; id: string; name?: string | null }): string {
  return actor.name || `${actor.type}:${actor.id}`
}
