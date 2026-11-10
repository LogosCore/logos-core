import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import type { CredentialValidity } from "@/graphql/gql/graphql"
import { CREDENTIAL_VALIDITIES, credentialValidity } from "./credential-validity-values"

// The icon on its own, sized for a table cell or a chip.
export function CredentialValidityIcon({
  validity,
  className,
}: {
  validity: CredentialValidity
  className?: string
}) {
  const { icon: Icon, className: tone } = credentialValidity(validity)
  return <Icon className={`${tone} ${className ?? "size-4"}`} />
}

// The footer control on the create/edit forms, where a Switch used to sit.
// A switch could only ask "is this valid?", which is what forced untested
// credentials to answer no.
export function CredentialValiditySelect({
  value,
  onValueChange,
  id,
  disabled,
}: {
  value: CredentialValidity
  onValueChange: (v: CredentialValidity) => void
  id?: string
  disabled?: boolean
}) {
  return (
    <div className="flex items-center gap-2 text-sm">
      <label htmlFor={id} className="text-muted-foreground">
        Validity
      </label>
      <Select
        value={value}
        onValueChange={(v) => onValueChange(v as CredentialValidity)}
        disabled={disabled}
      >
        <SelectTrigger id={id} className="w-40">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          {CREDENTIAL_VALIDITIES.map((v) => (
            <SelectItem key={v} value={v}>
              <span className="flex items-center gap-2">
                <CredentialValidityIcon
                  validity={v}
                  className="size-3.5 shrink-0"
                />
                {credentialValidity(v).label}
              </span>
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  )
}
