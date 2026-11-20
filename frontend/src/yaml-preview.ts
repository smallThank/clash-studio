import { parseDocument, Scalar, visit } from 'yaml';

const unicodeEscape = /\\(?:u([dD][89aAbB][0-9a-fA-F]{2})\\u([dD][c-fC-F][0-9a-fA-F]{2})|U([0-9a-fA-F]{8})|u([0-9a-fA-F]{4})|[^\r\n])/g;
const glyph = /[\p{L}\p{M}\p{N}\p{P}\p{S}\u200d]/u;

export function yamlPreviewText(source: string): string {
  if (!/\\[uU]/.test(source)) return source;
  const doc = parseDocument(source);
  if (doc.errors.length) return source;

  const replacements: { start: number; end: number; text: string }[] = [];
  visit(doc, {
    Scalar(_key, node) {
      if (node.type !== Scalar.QUOTE_DOUBLE || !node.range) return;
      const [start, end] = node.range;
      const original = source.slice(start, end);
      const text = original.replace(unicodeEscape, (
        escape, high: string | undefined, low: string | undefined, wide: string | undefined, bmp: string | undefined,
      ) => {
        if (high && low) {
          const value = 0x10000 + ((Number.parseInt(high, 16) - 0xd800) << 10) + Number.parseInt(low, 16) - 0xdc00;
          return visibleGlyph(value, escape);
        }
        const hex = wide ?? bmp;
        return hex ? visibleGlyph(Number.parseInt(hex, 16), escape) : escape;
      });
      if (text !== original) replacements.push({ start, end, text });
    },
  });

  let offset = 0;
  const result = replacements.map(({ start, end, text }) => {
    const fragment = source.slice(offset, start) + text;
    offset = end;
    return fragment;
  });
  return result.join('') + source.slice(offset);
}

function visibleGlyph(value: number, original: string): string {
  if (value < 0x80 || value > 0x10ffff) return original;
  const character = String.fromCodePoint(value);
  return glyph.test(character) ? character : original;
}
