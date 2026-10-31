import { DATE_TIME, formatDateTime } from "@/lib/date-format"

interface FormattedDateTimeTextProps
  extends React.HTMLAttributes<HTMLSpanElement> {
  date: string | Date
  /** Tokens as lib/date-format.ts takes them; the app's yyyy.mm.dd hh:mm:ss by default. */
  format?: string
}

export function FormattedDateTimeText({
  date,
  format = DATE_TIME,
  ...props
}: FormattedDateTimeTextProps) {
  return <span {...props}>{formatDateTime(date, format)}</span>
}
