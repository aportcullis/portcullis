import type { MessageInitShape } from "@bufbuild/protobuf";

import { ConnectionConfigInputSchema } from "@/gen/portcullis/v1/connections_pb";

// TlsMode is the accepted set (ADR-0014) — the same fixed enum the server
// validates. Keeping it a union (not string) lets the compiler catch a typo in
// a mode literal at the call site.
export type TlsMode = "verify-full" | "verify-ca" | "require" | "disable";

// ConfigDraft is the form's shape of a connection config. The password exists
// only in transient form state — the server never returns it (PRD §7.2).
export type ConfigDraft = {
  host: string;
  port: number;
  database: string;
  user: string;
  password: string;
  tlsMode: TlsMode;
};

export type TestResult = { ok: boolean; message: string };

// EnvironmentValue mirrors the server enum (connections.environment). A union,
// like TlsMode, so a typo'd literal fails to compile.
export type EnvironmentValue = "development" | "production";

// Environments with their form labels; development is the safe default.
export const ENVIRONMENTS: readonly { value: EnvironmentValue; label: string }[] = [
  { value: "development", label: "development" },
  { value: "production", label: "production" },
];

// parseEnvironment converts the string-valued DOM select boundary into the
// union (same rationale as parseTlsMode).
export const parseEnvironment = (value: string): EnvironmentValue | undefined =>
  ENVIRONMENTS.find((env) => env.value === value)?.value;

// TLS modes with their form labels; verify-full is the certificate-verifying
// default (PRD §8.1), require/disable are relaxed.
export const TLS_MODES: readonly { value: TlsMode; label: string }[] = [
  { value: "verify-full", label: "verify-full — validates certificate and hostname (default)" },
  { value: "verify-ca", label: "verify-ca — validates certificate, NOT the hostname" },
  { value: "require", label: "require — encrypts without certificate validation" },
  { value: "disable", label: "disable — no encryption" },
];

// parseTlsMode converts the string-valued DOM select boundary into the domain
// union. It keeps an unexpected option value from becoming trusted via a cast.
export const parseTlsMode = (value: string): TlsMode | undefined =>
  TLS_MODES.find((mode) => mode.value === value)?.value;

// isRelaxedTlsMode reports whether the mode skips certificate validation —
// choosing one is an explicit admin decision the server audits
// (CONNECTION_TLS_RELAXED). Single source for the create/edit warning and the
// list badge.
export const isRelaxedTlsMode = (mode: TlsMode): boolean =>
  mode === "require" || mode === "disable";

export const emptyDraft = (): ConfigDraft => ({
  host: "",
  port: 5432,
  database: "",
  user: "",
  password: "",
  tlsMode: "verify-full",
});

// toInput adapts a draft to the RPC message init shape the Connections client
// accepts (no cast — the init shape is exactly a plain object of these fields).
export function toInput(cfg: ConfigDraft): MessageInitShape<typeof ConnectionConfigInputSchema> {
  return {
    host: cfg.host,
    port: cfg.port,
    database: cfg.database,
    user: cfg.user,
    password: cfg.password,
    tlsMode: cfg.tlsMode,
  };
}
