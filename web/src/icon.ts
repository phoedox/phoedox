import { svg, type SVGTemplateResult } from "lit";
import { unsafeSVG } from "lit/directives/unsafe-svg.js";
import {
  ArrowLeft, ArrowRight, Bold, Bot, Box, Check, ChevronDown, Code, Copy, Heading1, Heading2, Heading3, History, Italic, Link2, List, ListOrdered, Quote, Redo2, SquareCode, Undo2, ExternalLink, FilePlus, FileText, FolderOpen, House,
  KeyRound, Link, ListChecks, LogIn, LogOut, Menu, Pin, Play, Search, Star, ShieldCheck, UserPlus, UserRound, Users, Pencil, Plus, Scale, Settings, Trash2, X,
  Cpu, Layers, DoorOpen, Router, RadioTower, Wifi, Network, EthernetPort, Phone, Rows4, Server, Cable, Plug, PlugZap, Lightbulb, Package, Workflow, Database, Monitor, Boxes, Container, Shapes, Tag, CircleDot, Waypoints, Paperclip, Upload, Image, Download, Table, CircleAlert, ChevronRight, Maximize2, Building2, Share2, Lock, LockOpen, UserMinus, Clock, Globe, Archive, RotateCcw, Webhook, Send, Puzzle, Store, BadgeCheck, TriangleAlert, ScrollText, Power, BatteryCharging, Save, Minus, FileCode, BetweenHorizontalEnd, BetweenVerticalEnd, TableRowsSplit, TableColumnsSplit, FilePen, Minimize2, FolderPlus, Folder, SquareKanban, CalendarDays, Flag, GripVertical, Columns3, ArrowUp, ArrowDown, Languages, Info, MessageSquareWarning, OctagonAlert, User, Heart, Bookmark, CircleCheck, Zap, Flame, Rocket, Shield, Sparkles, Calendar, Timer, Sigma, SquarePlay, GitBranch, Frame, Type, Highlighter, Underline, Strikethrough, Palette, ListTodo, Heading, TextQuote, Hourglass, Link2Off, Crosshair, Wrench, BookOpenCheck,
  MousePointer2, PenTool, MapPin, Ruler, ImageUp, ZoomIn, ZoomOut, Maximize, LandPlot,
  CircleDashed, RefreshCw, Keyboard, Camera, BrickWall, Import, DoorClosed, Rotate3d,
  Braces, Activity, Reply, MessageSquare, MessageSquarePlus, Bell, BellOff, BellRing, AtSign, SquareCheckBig, CalendarClock, CircleArrowUp, ShieldAlert, FileArchive, ArrowRightLeft,
} from "lucide";

type Node = [string, Record<string, string | number>][];

// Only imported icons end up in the bundle. Add new ones here.
const icons: Record<string, Node> = {
  ArrowLeft, ArrowRight, Bold, Bot, Box, Check, ChevronDown, Code, Copy, Heading1, Heading2, Heading3, History, Italic, Link2, List, ListOrdered, Quote, Redo2, SquareCode, Undo2, ExternalLink, FilePlus, FileText, FolderOpen, House,
  KeyRound, Link, ListChecks, LogIn, LogOut, Menu, Pin, Play, Search, Star, ShieldCheck, UserPlus, UserRound, Users, Pencil, Plus, Scale, Settings, Trash2, X,
  Cpu, Layers, DoorOpen, Router, RadioTower, Wifi, Network, EthernetPort, Phone, Rows4, Server, Cable, Plug, PlugZap, Lightbulb, Package, Workflow, Database, Monitor, Boxes, Container, Shapes, Tag, CircleDot, Waypoints, Paperclip, Upload, Image, Download, Table, CircleAlert, ChevronRight, Maximize2, Building2, Share2, Lock, LockOpen, UserMinus, Clock, Globe, Archive, RotateCcw, Webhook, Send, Puzzle, Store, BadgeCheck, TriangleAlert, ScrollText, Power, BatteryCharging, Save, Minus, FileCode, BetweenHorizontalEnd, BetweenVerticalEnd, TableRowsSplit, TableColumnsSplit, FilePen, Minimize2, FolderPlus, Folder, SquareKanban, CalendarDays, Flag, GripVertical, Columns3, ArrowUp, ArrowDown, Languages, Info, MessageSquareWarning, OctagonAlert, User, Heart, Bookmark, CircleCheck, Zap, Flame, Rocket, Shield, Sparkles, Calendar, Timer, Sigma, SquarePlay, GitBranch, Frame, Type, Highlighter, Underline, Strikethrough, Palette, ListTodo, Heading, TextQuote, Hourglass, Link2Off, Crosshair, Wrench, BookOpenCheck,
  MousePointer2, PenTool, MapPin, Ruler, ImageUp, ZoomIn, ZoomOut, Maximize, LandPlot,
  CircleDashed, RefreshCw, Keyboard, Camera, BrickWall, Import, DoorClosed, Rotate3d,
  Braces, Activity, Reply, MessageSquare, MessageSquarePlus, Bell, BellOff, BellRing, AtSign, SquareCheckBig, CalendarClock, CircleArrowUp, ShieldAlert, FileArchive, ArrowRightLeft,
} as unknown as Record<string, Node>;

/** Renders a Lucide icon by its PascalCase name. */
export function icon(name: keyof typeof icons | string, size = 16): SVGTemplateResult {
  // Unknown names (e.g. icons named by plugins) fall back to the puzzle piece.
  const inner = (icons[name] ?? icons.Puzzle).map(([tag, attrs]) => `<${tag} ${Object.entries(attrs).map(([k, v]) => `${k}="${v}"`).join(" ")}/>`).join("");
  return svg`<svg class="ico" width=${size} height=${size} viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${unsafeSVG(inner)}</svg>`;
}

export const iconNames = Object.keys(icons);

/** The SVG markup of an icon as a string (for Markdown rendering). */
export function iconSVG(name: string, size = 16): string {
  const inner = (icons[name] ?? icons.Puzzle).map(([tag, attrs]) => `<${tag} ${Object.entries(attrs).map(([k, v]) => `${k}="${v}"`).join(" ")}/>`).join("");
  return `<svg xmlns="http://www.w3.org/2000/svg" class="ico" width="${size}" height="${size}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${inner}</svg>`;
}

/** Icons offered for callouts. */
export const calloutIcons = ["Info", "Lightbulb", "MessageSquareWarning", "TriangleAlert", "OctagonAlert", "CircleCheck", "User", "Users", "Star", "Heart",
  "Bookmark", "Flag", "Pin", "Clock", "Calendar", "Zap", "Flame", "Rocket", "Shield", "Lock", "KeyRound", "Sparkles", "Bot", "Server", "Database", "Code"];
