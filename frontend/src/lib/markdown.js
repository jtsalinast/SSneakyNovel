import { marked } from 'marked';
import DOMPurify from 'dompurify';

marked.setOptions({
  gfm: true,
  breaks: true,
});

// Render markdown as safe HTML（DOMPurify sanitization to prevent AI scripts embedded in AI output）
export function renderMarkdown(text) {
  if (!text) return '';
  try {
    return DOMPurify.sanitize(marked.parse(text));
  } catch {
    return text;
  }
}
