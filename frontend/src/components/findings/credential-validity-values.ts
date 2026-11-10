import {
  CheckCircle2Icon,
  CircleDashedIcon,
  XCircleIcon,
  type LucideIcon,
} from "lucide-react"
import type { CredentialValidity } from "@/graphql/gql/graphql"

// A credential's validity as the UI names it: the states in order, and each
// one's label, description, icon and tone (credential-validity-utils.tsx
// draws them).

// The three states in the order they're offered in menus and pickers:
// unknown first, because it is where every credential starts.
export const CREDENTIAL_VALIDITIES: CredentialValidity[] = [
  "UNKNOWN",
  "VALID",
  "INVALID",
]

export interface ValidityPresentation {
  label: string
  // The longer form, for tooltips: says what the state means rather than
  // naming it, since "Unknown" on its own reads like missing data.
  description: string
  icon: LucideIcon
  // Icon colour. UNKNOWN is deliberately neutral — untested is not a
  // failure, and colouring it like one is the confusion this state exists
  // to end.
  className: string
}

const PRESENTATION: Record<CredentialValidity, ValidityPresentation> = {
  UNKNOWN: {
    label: "Untested",
    description: "Recorded, but nobody has tried it yet",
    icon: CircleDashedIcon,
    className: "text-muted-foreground/60",
  },
  VALID: {
    label: "Valid",
    description: "Used successfully against the target",
    icon: CheckCircle2Icon,
    className: "text-emerald-600 dark:text-emerald-400",
  },
  INVALID: {
    label: "Invalid",
    description: "Tried, and the target rejected it",
    icon: XCircleIcon,
    className: "text-destructive",
  },
}

export function credentialValidity(v: CredentialValidity): ValidityPresentation {
  return PRESENTATION[v] ?? PRESENTATION.UNKNOWN
}
