export function conversationMarkdown(title, messages) {
  const lines = [`# ${String(title || 'Conversation').replace(/[\r\n]+/g, ' ')}\n`];
  for (const message of messages ?? []) {
    if (message.role !== 'user' && message.role !== 'assistant') continue;
    const parts = (message.parts ?? []).filter((part) => part.type === 'text' && !part.data?.hidden);
    const text = parts.map((part) => part.data?.text ?? '').filter(Boolean).join('\n\n');
    if (text.trim()) lines.push(`## ${message.role === 'user' ? 'You' : 'Assistant'}\n\n${text}\n`);
  }
  return lines.join('\n');
}
