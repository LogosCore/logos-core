import type { ModuleConfigScope } from "@/graphql/gql/graphql"

export function scopeLabel(scope: ModuleConfigScope, moduleName: string): string {
  return scope === "MODULE" ? `All ${moduleName || "module"} instances` : "This instance only"
}

export function sourceLabel(source: string, moduleName: string): string {
  switch (source) {
    case "module":
      return `All ${moduleName || "module"} instances`
    case "instance":
      return "This instance"
    default:
      return "Default"
  }
}

export function actorLabel(actor: { type: string; id: string; name?: string | null }): string {
  return actor.name || `${actor.type}:${actor.id}`
}
