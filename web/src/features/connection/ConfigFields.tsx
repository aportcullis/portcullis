import type { Component } from "solid-js";
import { For, Show } from "solid-js";

import type { ConfigDraft } from "@/entities/connection/model";
import { TLS_MODES, isRelaxedTlsMode, parseTlsMode } from "@/entities/connection/model";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { TextField, TextFieldInput, TextFieldLabel } from "@/shared/ui/text-field";

// ConfigFields is the PG connection form body (PRD §7.2 — host/port/database/
// user/password/TLS mode), shared by the create and edit dialogs. There is no
// partial credential edit by design (ADR-0014): every use of these fields is a
// full config entry.
export const ConfigFields: Component<{
  draft: () => ConfigDraft;
  onPatch: (p: Partial<ConfigDraft>) => void;
}> = (props) => (
  <>
    <div class="grid grid-cols-3 gap-3">
      <TextField class="col-span-2">
        <TextFieldLabel for="conn-host">Host</TextFieldLabel>
        <TextFieldInput
          id="conn-host"
          required
          value={props.draft().host}
          onInput={(e) => props.onPatch({ host: e.currentTarget.value })}
        />
      </TextField>
      <TextField>
        <TextFieldLabel for="conn-port">Port</TextFieldLabel>
        <TextFieldInput
          id="conn-port"
          type="number"
          min="1"
          max="65535"
          required
          value={props.draft().port}
          onInput={(e) => props.onPatch({ port: Number(e.currentTarget.value) })}
        />
      </TextField>
    </div>
    <TextField>
      <TextFieldLabel for="conn-database">Database</TextFieldLabel>
      <TextFieldInput
        id="conn-database"
        required
        value={props.draft().database}
        onInput={(e) => props.onPatch({ database: e.currentTarget.value })}
      />
    </TextField>
    <div class="grid grid-cols-2 gap-3">
      <TextField>
        <TextFieldLabel for="conn-user">User</TextFieldLabel>
        <TextFieldInput
          id="conn-user"
          autocomplete="off"
          required
          value={props.draft().user}
          onInput={(e) => props.onPatch({ user: e.currentTarget.value })}
        />
      </TextField>
      <TextField>
        <TextFieldLabel for="conn-password">Password</TextFieldLabel>
        <TextFieldInput
          id="conn-password"
          type="password"
          autocomplete="new-password"
          value={props.draft().password}
          onInput={(e) => props.onPatch({ password: e.currentTarget.value })}
        />
      </TextField>
    </div>
    <div class="flex flex-col gap-1">
      <label class="text-sm font-medium leading-none" for="conn-tls">
        TLS mode
      </label>
      <select
        id="conn-tls"
        class="flex h-10 w-full rounded-md border border-input bg-transparent px-3 py-2 text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
        value={props.draft().tlsMode}
        onChange={(e) => {
          const tlsMode = parseTlsMode(e.currentTarget.value);
          if (tlsMode) props.onPatch({ tlsMode });
        }}
      >
        <For each={TLS_MODES}>{(mode) => <option value={mode.value}>{mode.label}</option>}</For>
      </select>
    </div>
    <Show when={isRelaxedTlsMode(props.draft().tlsMode)}>
      <Alert variant="destructive">
        <AlertDescription>
          This mode skips certificate validation — the connection is exposed to
          man-in-the-middle attacks. Choosing it is recorded in the audit trail.
        </AlertDescription>
      </Alert>
    </Show>
  </>
);
