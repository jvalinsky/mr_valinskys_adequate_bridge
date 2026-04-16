// OpenCode Custom Tool: Deciduous Decision Graph
// Wraps the deciduous CLI for direct graph operations from OpenCode
//
// This tool allows agents to interact with the decision graph without
// needing to use the bash tool directly.

import { tool } from "@opencode-ai/plugin"

export default tool({
  description: "Manage the deciduous decision graph - add nodes, create edges, query the graph, and sync",
  args: {
    command: tool.schema.string().describe(
      "The deciduous subcommand and arguments to run. Examples: " +
      "'add goal \"Title\" -c 90', " +
      "'link 1 2 -r \"reason\"', " +
      "'nodes', 'edges', 'graph', 'pulse', 'sync'"
    ),
  },
  async execute(args, context) {
    const parsed = splitCommand(args.command)
    if (parsed instanceof Error) {
      return `Error: ${parsed.message}`
    }

    const proc = Bun.spawn(["deciduous", ...parsed], {
      cwd: context.directory,
      stdout: "pipe",
      stderr: "pipe",
    })

    const stdout = await new Response(proc.stdout).text()
    const stderr = await new Response(proc.stderr).text()
    const exitCode = await proc.exited

    if (exitCode !== 0) {
      return `Error (exit ${exitCode}):\n${stderr}\n${stdout}`
    }

    return stdout || "(no output)"
  },
})

function splitCommand(command: string): string[] | Error {
  const result: string[] = []
  let current = ""
  let quote: '"' | "'" | null = null
  let escaping = false

  for (const ch of command) {
    if (escaping) {
      current += ch
      escaping = false
      continue
    }

    if (ch === "\\") {
      escaping = true
      continue
    }

    if (quote) {
      if (ch === quote) {
        quote = null
      } else {
        current += ch
      }
      continue
    }

    if (ch === '"' || ch === "'") {
      quote = ch
      continue
    }

    if (/\s/.test(ch)) {
      if (current !== "") {
        result.push(current)
        current = ""
      }
      continue
    }

    current += ch
  }

  if (escaping) {
    return new Error("dangling escape in command")
  }
  if (quote) {
    return new Error("unterminated quote in command")
  }
  if (current !== "") {
    result.push(current)
  }
  return result
}
