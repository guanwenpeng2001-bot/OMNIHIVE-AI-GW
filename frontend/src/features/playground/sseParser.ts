export interface ParsedSseRecord {
  id: string
  event: string
  data: string
}

export class PlaygroundSseParser {
  private buffer = ''
  private eventId = ''
  private eventName = 'message'
  private dataLines: string[] = []

  feed(chunk: string): ParsedSseRecord[] {
    this.buffer += chunk
    const emitted: ParsedSseRecord[] = []

    let newlineIdx = this.buffer.indexOf('\n')
    while (newlineIdx !== -1) {
      let line = this.buffer.slice(0, newlineIdx)
      this.buffer = this.buffer.slice(newlineIdx + 1)
      if (line.endsWith('\r')) {
        line = line.slice(0, -1)
      }
      this.processLine(line, emitted)
      newlineIdx = this.buffer.indexOf('\n')
    }

    return emitted
  }

  flush(): ParsedSseRecord[] {
    const emitted: ParsedSseRecord[] = []
    if (this.buffer.length > 0) {
      const trailing = this.buffer.endsWith('\r') ? this.buffer.slice(0, -1) : this.buffer
      this.buffer = ''
      this.processLine(trailing, emitted)
    }
    this.dispatchCurrent(emitted)
    return emitted
  }

  private processLine(line: string, emitted: ParsedSseRecord[]): void {
    if (line === '') {
      this.dispatchCurrent(emitted)
      return
    }
    if (line.startsWith(':')) {
      return
    }
    const colonIdx = line.indexOf(':')
    const field = colonIdx === -1 ? line : line.slice(0, colonIdx)
    let value = colonIdx === -1 ? '' : line.slice(colonIdx + 1)
    if (value.startsWith(' ')) {
      value = value.slice(1)
    }

    if (field === 'event') {
      this.eventName = value
    } else if (field === 'id') {
      if (!value.includes('\0')) {
        this.eventId = value
      }
    } else if (field === 'data') {
      this.dataLines.push(value)
    }
  }

  private dispatchCurrent(emitted: ParsedSseRecord[]): void {
    if (this.dataLines.length === 0) {
      this.eventName = 'message'
      return
    }
    emitted.push({
      id: this.eventId,
      event: this.eventName || 'message',
      data: this.dataLines.join('\n')
    })
    this.eventName = 'message'
    this.dataLines = []
  }
}
