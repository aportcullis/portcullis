import { z } from 'zod';

const jsonValue = z.json();
const jsonText = z.codec(z.string(), jsonValue, {
  decode: text => {
    const value: unknown = JSON.parse(text);
    return jsonValue.parse(value);
  },
  encode: value => JSON.stringify(value),
});

/** Decodes JSON and validates its schema without exposing the payload in errors. */
export function decodeJSON<T>(schema: { parse(value: unknown): T }, text: string): T {
  try {
    return schema.parse(z.decode(jsonText, text));
  } catch {
    throw new Error('Invalid JSON contract');
  }
}

/** Validates a typed value before encoding it as JSON. */
export function encodeJSON<T extends z.core.$ZodType>(schema: T, value: z.input<T>): string {
  try {
    return z.encode(jsonText, z.parse(jsonValue, z.parse(schema, value)));
  } catch {
    throw new Error('Invalid JSON contract');
  }
}
