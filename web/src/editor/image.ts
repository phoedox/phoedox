// Inline images in the live editor, stored as Markdown ![alt](src).
import type { TextMatchTransformer } from "@lexical/markdown";
import { $applyNodeReplacement, DecoratorNode, type DOMExportOutput, type EditorConfig, type LexicalNode, type NodeKey, type SerializedLexicalNode, type Spread } from "lexical";

type SerializedImageNode = Spread<{ src: string; alt: string }, SerializedLexicalNode>;

/** Only images from this instance (uploaded files) or https are shown. */
export function safeImageSrc(src: string): boolean {
  return /^\/api\/v1\/files\/[A-Za-z0-9]+(\/[^\s"'<>]*)?$/.test(src) || /^https:\/\/[^\s"'<>]+$/.test(src);
}

export class ImageNode extends DecoratorNode<null> {
  __src: string;
  __alt: string;

  static getType() { return "image"; }
  static clone(node: ImageNode) { return new ImageNode(node.__src, node.__alt, node.__key); }
  static importJSON(json: SerializedImageNode) { return $createImageNode(json.src, json.alt); }

  constructor(src = "", alt = "", key?: NodeKey) {
    super(key);
    this.__src = src;
    this.__alt = alt;
  }

  exportJSON(): SerializedImageNode {
    return { ...super.exportJSON(), type: "image", version: 1, src: this.__src, alt: this.__alt };
  }

  createDOM(config: EditorConfig): HTMLElement {
    const span = document.createElement("span");
    span.className = config.theme.image ?? "ed-img";
    if (safeImageSrc(this.__src)) {
      const img = document.createElement("img");
      img.src = this.__src;
      img.alt = this.__alt;
      img.loading = "lazy";
      img.draggable = false;
      span.append(img);
    } else {
      span.textContent = `![${this.__alt}](${this.__src})`;
    }
    return span;
  }

  updateDOM(prev: ImageNode): boolean {
    return prev.__src !== this.__src || prev.__alt !== this.__alt;
  }

  exportDOM(): DOMExportOutput {
    const img = document.createElement("img");
    img.src = this.__src;
    img.alt = this.__alt;
    return { element: img };
  }

  getSrc() { return this.getLatest().__src; }
  getAlt() { return this.getLatest().__alt; }
  isInline() { return true; }
  decorate() { return null; }
}

export function $createImageNode(src: string, alt: string): ImageNode {
  return $applyNodeReplacement(new ImageNode(src, alt));
}

export function $isImageNode(node: LexicalNode | null | undefined): node is ImageNode {
  return node instanceof ImageNode;
}

const escapeAlt = (s: string) => s.replace(/[[\]\\]/g, "");

export const IMAGE: TextMatchTransformer = {
  dependencies: [ImageNode],
  export: (node) => ($isImageNode(node) ? `![${escapeAlt(node.getAlt())}](${node.getSrc()})` : null),
  importRegExp: /!\[([^\]]*)\]\(([^()\s]+)\)/,
  regExp: /!\[([^\]]*)\]\(([^()\s]+)\)$/,
  replace: (textNode, match) => {
    textNode.replace($createImageNode(match[2], match[1]));
  },
  trigger: ")",
  type: "text-match",
};
