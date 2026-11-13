import { svg, type SVGTemplateResult } from "lit";
import { unsafeSVG } from "lit/directives/unsafe-svg.js";
import {
  ArrowLeft, ArrowRight, Bold, Bot, Box, Check, ChevronDown, Code, Copy, Heading1, Heading2, Heading3, History, Italic, Link2, List, ListOrdered, Quote, Redo2, SquareCode, Undo2, ExternalLink, FilePlus, FileText, FolderOpen, House,
  KeyRound, Link, ListChecks, LogIn, LogOut, Menu, Pin, Play, Search, Star, ShieldCheck, UserPlus, UserRound, Users, Pencil, Plus, Scale, Settings, Trash2, X,
  Cpu, Layers, DoorOpen, Router, RadioTower, Wifi, Network, EthernetPort, Phone, Rows4, Server, Cable, Plug, PlugZap, Lightbulb, Package, Workflow, Database, Monitor, Boxes, Container, Shapes, Tag, CircleDot, Waypoints, Paperclip, Upload, Image, Download, Table, CircleAlert, ChevronRight, Maximize2, Building2, Share2, Lock, LockOpen, UserMinus, Clock, Globe, Archive, RotateCcw, Webhook, Send,
} from "lucide";

type Node = [string, Record<string, string | number>][];

// Only imported icons end up in the bundle. Add new ones here.
const icons: Record<string, Node> = {
  ArrowLeft, ArrowRight, Bold, Bot, Box, Check, ChevronDown, Code, Copy, Heading1, Heading2, Heading3, History, Italic, Link2, List, ListOrdered, Quote, Redo2, SquareCode, Undo2, ExternalLink, FilePlus, FileText, FolderOpen, House,
  KeyRound, Link, ListChecks, LogIn, LogOut, Menu, Pin, Play, Search, Star, ShieldCheck, UserPlus, UserRound, Users, Pencil, Plus, Scale, Settings, Trash2, X,
  Cpu, Layers, DoorOpen, Router, RadioTower, Wifi, Network, EthernetPort, Phone, Rows4, Server, Cable, Plug, PlugZap, Lightbulb, Package, Workflow, Database, Monitor, Boxes, Container, Shapes, Tag, CircleDot, Waypoints, Paperclip, Upload, Image, Download, Table, CircleAlert, ChevronRight, Maximize2, Building2, Share2, Lock, LockOpen, UserMinus, Clock, Globe, Archive, RotateCcw, Webhook, Send,
} as unknown as Record<string, Node>;

/** Renders a Lucide icon by its PascalCase name. */
export function icon(name: keyof typeof icons | string, size = 16): SVGTemplateResult {
  const inner = (icons[name] ?? []).map(([tag, attrs]) => `<${tag} ${Object.entries(attrs).map(([k, v]) => `${k}="${v}"`).join(" ")}/>`).join("");
  return svg`<svg class="ico" width=${size} height=${size} viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${unsafeSVG(inner)}</svg>`;
}

export const iconNames = Object.keys(icons);
