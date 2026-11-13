import DOMPurify from "dompurify";
import { marked } from "marked";

// Same rule as the server (internal/graph): a mention starts the text or
// follows a character that cannot be part of an e-mail address.
const mentionRe = /(^|[^\w@./-])@(?:([a-z0-9-]+)\/)?([a-z0-9]+(?:-[a-z0-9]+)*)/g;

/** Replaces @mentions with links to the entity page. */
export function linkMentions(md: string, project: string): string {
  return md.replace(mentionRe, (_m, pre: string, proj: string | undefined, slug: string) => {
    const p = proj ?? project;
    return `${pre}[@${proj ? proj + "/" : ""}${slug}](/p/${p}/e/${slug} "mention")`;
  });
}

/** Renders Markdown to sanitised HTML. */
export function renderMarkdown(md: string, project: string): string {
  const html = marked.parse(linkMentions(md ?? "", project), { async: false }) as string;
  return DOMPurify.sanitize(html, { ADD_ATTR: ["target"] });
}
