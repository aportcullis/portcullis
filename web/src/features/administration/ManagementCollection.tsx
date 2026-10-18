import { For, Show, createEffect, onCleanup } from "solid-js";
import { useSearchParams } from "@solidjs/router";

import { createManagementQuery } from "@/entities/administration/query";
import { listManagement, type ManagementKind, type ManagementList } from "@/entities/administration/api";
import { hasPermission, load } from "@/entities/session/store";
import { DeleteRoleAction } from "@/features/administration/RoleManagement";
import { RevokeBindingAction } from "@/features/administration/BindingManagement";
import { UserStatusAction } from "@/features/administration/UserManagement";
import { ManagementFeedback, ManagementSelect } from "@/features/administration/controls";
import { Button } from "@/shared/ui/button";
import { LoadingSkeleton } from "@/shared/ui/LoadingSkeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/shared/ui/table";

/** Shows one authorized numbered collection without borrowing detail permissions. */
export function ManagementCollection(props: { kind: ManagementKind; actorID: string; onAdmissionFailure: () => void }) {
  const query = createManagementQuery<ManagementList>(() => void load());
  const [search, setSearch] = useSearchParams();
  const page = () => { const value = Number(search.page); return Number.isInteger(value) && value > 0 && value <= 4294967295 ? value : 1; };
  const pageSize = () => { const value = Number(search.size); return [10, 20, 50, 100].includes(value) ? value : 20; };
  const revision = () => typeof search.revision === "string" && /^[1-9][0-9]{0,18}$/.test(search.revision) ? BigInt(search.revision) : 0n;
  /** Reloads the first page when a count or authority revision has changed. */
  const refresh = () => {
    if (page() !== 1 || revision() !== 0n) setSearch({ page: undefined, revision: undefined });
    else void query.load(() => listManagement(props.kind, 1, pageSize(), 0n));
  };
  createEffect(() => { const requestedPage = page(); const size = pageSize(); const expected = revision(); void query.load(() => listManagement(props.kind, requestedPage, size, expected)); });
  onCleanup(query.reset);
  /** Retains exact count arithmetic even beyond JavaScript safe integers. */
  const range = () => {
    const info = query.data()?.pagination;
    if (!info) return "";
    const start = info.totalCount === 0n ? 0n : BigInt(info.page - 1) * BigInt(info.pageSize) + 1n;
    const upper = BigInt(info.page) * BigInt(info.pageSize);
    const end = upper < info.totalCount ? upper : info.totalCount;
    return `${start}–${end} of ${info.totalCount}`;
  };
  return <section class="flex min-w-0 flex-col gap-4" aria-label="Management list">
    <div class="flex flex-wrap items-end gap-3"><Button variant="outline" disabled={query.loading()} onClick={refresh}>Refresh</Button><ManagementSelect label="Page size" value={String(pageSize())} onChange={value => setSearch({ size: value, page: undefined, revision: undefined })}><For each={[10, 20, 50, 100]}>{size => <option value={size}>{size}</option>}</For></ManagementSelect></div>
    <ManagementFeedback error={query.error()} />
    <Show when={query.loading()}><LoadingSkeleton label="Loading management records…" /></Show>
    <Show when={query.data()}>{result => <>
      <Show when={result().rows.length > 0} fallback={<p>No records on this page.</p>}>
        <div class="max-w-full overflow-x-auto"><Table><TableHeader><TableRow><TableHead>{props.kind === "users" ? "Email" : props.kind === "roles" ? "Name" : "User → Role"}</TableHead><TableHead>Status</TableHead><TableHead>Actions</TableHead></TableRow></TableHeader><TableBody><For each={result().rows}>{row => <TableRow>
          <TableCell><div class="max-w-xs break-all">{row.label}</div><small class="block max-w-xs break-all font-mono">{row.id}</small><Show when={row.scope}><small class="block max-w-xs break-all">Scope: {row.scope}</small></Show><Show when={row.version > 0n}><small class="block">Version {row.version.toString()}</small></Show><Show when={props.kind === "roles"}><small>{row.bindingCount.toString()} live Bindings</small></Show></TableCell>
          <TableCell>{row.status}</TableCell><TableCell><div class="flex flex-wrap gap-2">
            <Show when={hasPermission(`${props.kind}.get`)}><a class="text-sm underline" href={`/${props.kind}/${row.id}`}>Details</a></Show>
            <Show when={props.kind === "users"}><UserStatusAction id={row.id} status={row.status} own={row.userID === props.actorID} onAdmissionFailure={props.onAdmissionFailure} /></Show>
            <Show when={props.kind === "roles"}><DeleteRoleAction id={row.id} version={row.version} isSystem={row.isSystem} bindingCount={row.bindingCount} onAdmissionFailure={props.onAdmissionFailure} /></Show>
            <Show when={props.kind === "bindings"}><RevokeBindingAction id={row.id} version={row.version} own={row.userID === props.actorID} revoked={row.status === "Revoked"} onAdmissionFailure={props.onAdmissionFailure} /></Show>
          </div></TableCell></TableRow>}</For></TableBody></Table></div>
      </Show><p class="text-sm">{range()}</p><nav aria-label="Management pages" class="flex flex-wrap items-center gap-3">
        <Button variant="outline" disabled={query.loading() || page() <= 1} onClick={() => setSearch({ page: page() - 1, revision: result().pagination?.authorizationRevision.toString() })}>Previous</Button><span>Page {page()}</span>
        <Button variant="outline" disabled={query.loading() || !result().pagination || BigInt(page()) * BigInt(pageSize()) >= (result().pagination?.totalCount ?? 0n)} onClick={() => setSearch({ page: page() + 1, revision: result().pagination?.authorizationRevision.toString() })}>Next</Button>
      </nav>
    </>}</Show>
  </section>;
}
