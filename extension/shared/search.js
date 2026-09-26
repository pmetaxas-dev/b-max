// "Where do I start?": the server sends search PHRASES, never links; the only
// URL the extension ever builds from them is this search page.
const MAX_QUERY = 200;

export function searchUrl(query) {
  const q = typeof query === 'string' ? query.trim() : '';
  if (!q || q.length > MAX_QUERY) return null;
  return `https://www.google.com/search?q=${encodeURIComponent(q)}`;
}
