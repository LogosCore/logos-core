// Shell/batch/PowerShell rendering for the attachment preview panel.
//
// Like the YAML renderer, this is line-based and does not parse the script's
// grammar — comments, strings, keywords and variables are highlighted where
// they stand, which is enough for a read-only preview of an ops script. A
// half-written or syntactically broken file still renders, because someone
// looking at a broken script is precisely the person who needs the preview.
//
// The highlighter auto-detects the dialect from the shebang or falls back to
// POSIX shell, which covers bash/zsh/ksh well enough. Batch and PowerShell
// get their own keyword and comment patterns.

import {
  escapeHtml,
  noteBody,
  type PreviewBody,
} from "./wiki-file-preview-document"

const EMPTY_FILE = "This file is empty."
const MAX_RENDERED_CHARS = 2 * 1024 * 1024

type Dialect = "shell" | "batch" | "powershell"

const SHELL_KEYWORDS = new Set([
  "if", "then", "else", "elif", "fi", "for", "while", "until", "do", "done",
  "case", "esac", "in", "function", "select", "return", "exit", "break",
  "continue", "local", "export", "readonly", "declare", "typeset", "unset",
  "shift", "trap", "source", "eval", "exec", "set",
])

const BATCH_KEYWORDS = new Set([
  "if", "else", "for", "in", "do", "goto", "call", "exit", "set", "setlocal",
  "endlocal", "echo", "pause", "rem", "cls", "cd", "pushd", "popd", "mkdir",
  "rmdir", "del", "copy", "move", "ren", "type", "find", "findstr", "sort",
  "start", "net", "defined", "not", "equ", "neq", "lss", "leq", "gtr", "geq",
  "exist", "errorlevel", "off", "on",
])

const PS_KEYWORDS = new Set([
  "if", "else", "elseif", "switch", "foreach", "for", "while", "do", "until",
  "break", "continue", "return", "exit", "throw", "try", "catch", "finally",
  "function", "filter", "param", "begin", "process", "end", "class", "enum",
  "using", "import-module", "export-modulemember", "new-object",
  "write-host", "write-output", "write-error", "write-warning",
  "get-childitem", "set-location", "get-content", "set-content",
  "invoke-command", "invoke-expression",
])

function detectDialect(text: string): Dialect {
  const firstLine = text.slice(0, text.indexOf("\n")).trim()

  if (/^#!.*\b(bash|sh|zsh|ksh|fish|dash)\b/.test(firstLine)) return "shell"
  if (/^#!.*\bpwsh\b/.test(firstLine)) return "powershell"
  if (/^@?echo\s+off/i.test(firstLine)) return "batch"
  if (/^#!.*\benv\s/.test(firstLine)) return "shell"

  return "shell"
}

export function renderScriptPreview(text: string): PreviewBody {
  if (text.trim() === "") return noteBody(EMPTY_FILE)

  const truncated = text.length > MAX_RENDERED_CHARS
  const shown = truncated ? text.slice(0, MAX_RENDERED_CHARS) : text
  const dialect = detectDialect(shown)
  const lines = shown.split("\n")

  const numbered = lines
    .map((line, i) => {
      const num = span("line-num", String(i + 1))
      const code = highlightLine(line, dialect)
      return `<span class="script-line">${num}${code}</span>`
    })
    .join("\n")

  return {
    bodyHtml: `<pre class="script-pre">${numbered}</pre>`,
    notice: truncated
      ? "Shown to the first 2 MB. Download the file for the rest."
      : undefined,
  }
}

function highlightLine(line: string, dialect: Dialect): string {
  if (line.trim() === "") return escapeHtml(line)

  if (dialect === "batch") return highlightBatchLine(line)
  if (dialect === "powershell") return highlightPsLine(line)
  return highlightShellLine(line)
}

function highlightShellLine(line: string): string {
  const trimmed = line.trimStart()

  // Shebang
  if (trimmed.startsWith("#!")) return span("comment", line)

  // Full-line comment
  if (trimmed.startsWith("#")) return span("comment", line)

  // Split trailing comment (outside quotes)
  const { code, comment } = splitShellComment(line)
  return tokenizeShell(code) + (comment ? span("comment", comment) : "")
}

function splitShellComment(line: string): { code: string; comment: string } {
  let quote: string | null = null

  for (let i = 0; i < line.length; i++) {
    const ch = line[i]
    if (ch === "\\" && quote !== "'") { i++; continue }
    if (quote !== null) { if (ch === quote) quote = null; continue }
    if (ch === '"' || ch === "'") { quote = ch; continue }
    if (ch === "#" && i > 0 && /\s/.test(line[i - 1])) {
      return { code: line.slice(0, i), comment: line.slice(i) }
    }
  }
  return { code: line, comment: "" }
}

function tokenizeShell(code: string): string {
  const result: string[] = []
  let i = 0

  while (i < code.length) {
    const ch = code[i]

    // Whitespace
    if (/\s/.test(ch)) {
      let end = i + 1
      while (end < code.length && /\s/.test(code[end])) end++
      result.push(escapeHtml(code.slice(i, end)))
      i = end
      continue
    }

    // Escape
    if (ch === "\\") {
      result.push(escapeHtml(code.slice(i, i + 2)))
      i += 2
      continue
    }

    // Double-quoted string
    if (ch === '"') {
      const end = findClosingQuote(code, i, '"')
      result.push(span("string", code.slice(i, end)))
      i = end
      continue
    }

    // Single-quoted string
    if (ch === "'") {
      const end = findClosingQuote(code, i, "'")
      result.push(span("string", code.slice(i, end)))
      i = end
      continue
    }

    // Variable
    if (ch === "$") {
      const varMatch = /^\$\{[^}]*\}|\$[A-Za-z_][A-Za-z0-9_]*|\$[0-9@#?!*$-]/.exec(code.slice(i))
      if (varMatch) {
        result.push(span("variable", varMatch[0]))
        i += varMatch[0].length
        continue
      }
    }

    // Operators / redirects
    if (ch === "|" || ch === "&" || ch === ">" || ch === "<" || ch === ";") {
      let end = i + 1
      if ((ch === "|" || ch === "&") && code[i + 1] === ch) end++
      else if (ch === ">" && code[i + 1] === ">") end++
      else if (ch === ">" && code[i + 1] === "&") end++
      result.push(span("operator", code.slice(i, end)))
      i = end
      continue
    }

    // Word
    const wordMatch = /^[^\s\\'"$|&><;#]+/.exec(code.slice(i))
    if (wordMatch) {
      const word = wordMatch[0]
      if (SHELL_KEYWORDS.has(word)) {
        result.push(span("keyword", word))
      } else {
        result.push(escapeHtml(word))
      }
      i += word.length
      continue
    }

    result.push(escapeHtml(ch))
    i++
  }

  return result.join("")
}

function highlightBatchLine(line: string): string {
  const trimmed = line.trimStart()
  const lead = line.slice(0, line.length - trimmed.length)

  // REM comment or :: comment
  if (/^(rem\b|::)/i.test(trimmed)) {
    return span("comment", line)
  }

  // Label
  if (trimmed.startsWith(":") && !trimmed.startsWith("::")) {
    return escapeHtml(lead) + span("keyword", trimmed)
  }

  return escapeHtml(lead) + tokenizeBatch(trimmed)
}

function tokenizeBatch(code: string): string {
  const result: string[] = []
  let i = 0

  while (i < code.length) {
    const ch = code[i]

    if (/\s/.test(ch)) {
      let end = i + 1
      while (end < code.length && /\s/.test(code[end])) end++
      result.push(escapeHtml(code.slice(i, end)))
      i = end
      continue
    }

    // %variable%
    if (ch === "%") {
      const varMatch = /^%[~dpnx]*[0-9]|^%[A-Za-z_][A-Za-z0-9_]*%|^%%[A-Za-z]/.exec(code.slice(i))
      if (varMatch) {
        result.push(span("variable", varMatch[0]))
        i += varMatch[0].length
        continue
      }
    }

    // Quoted string
    if (ch === '"') {
      const end = findClosingQuote(code, i, '"')
      result.push(span("string", code.slice(i, end)))
      i = end
      continue
    }

    // Operators
    if (ch === "|" || ch === "&" || ch === ">" || ch === "<") {
      let end = i + 1
      if (ch === ">" && code[i + 1] === ">") end++
      result.push(span("operator", code.slice(i, end)))
      i = end
      continue
    }

    // Word
    const wordMatch = /^[^\s%"<>|&]+/.exec(code.slice(i))
    if (wordMatch) {
      const word = wordMatch[0]
      if (BATCH_KEYWORDS.has(word.toLowerCase())) {
        result.push(span("keyword", word))
      } else {
        result.push(escapeHtml(word))
      }
      i += word.length
      continue
    }

    result.push(escapeHtml(ch))
    i++
  }

  return result.join("")
}

function highlightPsLine(line: string): string {
  const trimmed = line.trimStart()

  // Full-line comment
  if (trimmed.startsWith("#")) return span("comment", line)

  // Block comment markers
  if (trimmed.startsWith("<#") || trimmed.startsWith("#>")) return span("comment", line)

  const { code, comment } = splitShellComment(line)
  return tokenizePs(code) + (comment ? span("comment", comment) : "")
}

function tokenizePs(code: string): string {
  const result: string[] = []
  let i = 0

  while (i < code.length) {
    const ch = code[i]

    if (/\s/.test(ch)) {
      let end = i + 1
      while (end < code.length && /\s/.test(code[end])) end++
      result.push(escapeHtml(code.slice(i, end)))
      i = end
      continue
    }

    // Variable
    if (ch === "$") {
      const varMatch = /^\$[A-Za-z_][A-Za-z0-9_]*/.exec(code.slice(i))
      if (varMatch) {
        result.push(span("variable", varMatch[0]))
        i += varMatch[0].length
        continue
      }
    }

    // Strings
    if (ch === '"' || ch === "'") {
      const end = findClosingQuote(code, i, ch)
      result.push(span("string", code.slice(i, end)))
      i = end
      continue
    }

    // Operators
    if (ch === "|" || ch === ">" || ch === "<") {
      let end = i + 1
      if (ch === ">" && code[i + 1] === ">") end++
      result.push(span("operator", code.slice(i, end)))
      i = end
      continue
    }

    // Parameter flags
    if (ch === "-" && /[A-Za-z]/.test(code[i + 1] ?? "")) {
      const paramMatch = /^-[A-Za-z][A-Za-z0-9]*/.exec(code.slice(i))
      if (paramMatch) {
        result.push(span("variable", paramMatch[0]))
        i += paramMatch[0].length
        continue
      }
    }

    // Word (cmdlet-style with dashes)
    const wordMatch = /^[A-Za-z_][A-Za-z0-9_-]*/.exec(code.slice(i))
    if (wordMatch) {
      const word = wordMatch[0]
      if (PS_KEYWORDS.has(word.toLowerCase())) {
        result.push(span("keyword", word))
      } else {
        result.push(escapeHtml(word))
      }
      i += word.length
      continue
    }

    result.push(escapeHtml(ch))
    i++
  }

  return result.join("")
}

function findClosingQuote(code: string, start: number, quote: string): number {
  for (let i = start + 1; i < code.length; i++) {
    if (code[i] === "\\" && quote === '"') { i++; continue }
    if (code[i] === quote) return i + 1
  }
  return code.length
}

type ScriptTokenKind = "keyword" | "string" | "variable" | "comment" | "operator" | "line-num"

function span(kind: ScriptTokenKind, token: string): string {
  return `<span class="sc-${kind}">${escapeHtml(token)}</span>`
}
