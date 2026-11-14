/** Fields of a control tool's input schema, without the target argument
 * Phoedox fills in. Enums become buttons, numbers number inputs. */
export interface ToolField {
  name: string;
  label: string;
  type: "enum" | "number" | "text" | "bool";
  options?: string[];
  min?: number;
  max?: number;
  required: boolean;
}

interface Schema { properties?: Record<string, { type?: string; enum?: unknown[]; description?: string; title?: string; minimum?: number; maximum?: number }>; required?: string[] }

export function toolFields(schema: unknown, targetArg: string): ToolField[] {
  const s = (schema && typeof schema === "object" ? schema : {}) as Schema;
  const req = new Set(s.required ?? []);
  const out: ToolField[] = [];
  for (const [name, p] of Object.entries(s.properties ?? {})) {
    if (name === targetArg) continue;
    const label = p.title || p.description || name;
    const f: ToolField = { name, label, type: "text", required: req.has(name) };
    if (Array.isArray(p.enum) && p.enum.every((v) => typeof v === "string")) {
      f.type = "enum";
      f.options = p.enum as string[];
    } else if (p.type === "integer" || p.type === "number") {
      f.type = "number";
      f.min = p.minimum;
      f.max = p.maximum;
    } else if (p.type === "boolean") {
      f.type = "bool";
    }
    out.push(f);
  }
  return out;
}

/** A tool whose only required input is one enum runs straight from a
 * button per value (on, off, toggle). */
export function quickEnum(fields: ToolField[]): ToolField | undefined {
  const enums = fields.filter((f) => f.type === "enum");
  return enums.length === 1 && fields.every((f) => f === enums[0] || !f.required) ? enums[0] : undefined;
}
