import { describe, expect, it } from 'vitest';
import { conversationMarkdown } from './export-conversation.js';

describe('conversation export', () => {
  it('keeps user and assistant text in order and skips hidden and tool messages', () => {
    const msg = (role, text, extra = {}) => ({ role, parts: [{ type: 'text', data: { text, ...extra } }] });
    const out = conversationMarkdown('Redis Project', [msg('user', 'Hello'), msg('assistant', 'Answer\n\n```js\ncode\n```'), msg('tool', 'private'), msg('user', 'hidden', { hidden: true }), msg('user', 'Last')]);
    const hello = out.indexOf('Hello');
    const answer = out.indexOf('```js\ncode\n```');
    const last = out.indexOf('Last');
    expect(hello).toBeGreaterThanOrEqual(0);
    expect(answer).toBeGreaterThan(hello);
    expect(last).toBeGreaterThan(answer);
    expect(out).not.toContain('private');
    expect(out).not.toContain('hidden');
  });
  it('preserves indentation in message text', () => {
    expect(conversationMarkdown('Code', [{ role: 'assistant', parts: [{ type: 'text', data: { text: '    indented code' } }] }])).toContain('\n\n    indented code\n');
  });
  it('keeps old and new messages in a long conversation', () => {
    const messages = Array.from({ length: 601 }, (_, i) => ({ role: i % 2 ? 'assistant' : 'user', parts: [{ type: 'text', data: { text: `message ${i}` } }] }));
    const result = conversationMarkdown('Long chat', messages);
    expect(result).toContain('message 0');
    expect(result).toContain('message 600');
    expect((result.match(/^## /gm) ?? [])).toHaveLength(601);
  });
});
