import type { Component } from "solid-js";
import { Show, createSignal } from "solid-js";

import { archiveConnection, errorMessage } from "@/entities/connection/store";
import { createDialogSession } from "@/shared/lib/dialogSession";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/shared/ui/dialog";

// ArchiveConnectionDialog confirms the soft delete. It spells out the PRD §4.3 consequences: the stored credential is destroyed, so restoring later means re-entering it and passing a fresh connection test.
export const ArchiveConnectionDialog: Component<{ id: string; displayName: string }> = (props) => {
  const { discardSession, runInSession } = createDialogSession();
  const [open, setOpen] = createSignal(false);
  const [error, setError] = createSignal("");
  const [pending, setPending] = createSignal(false);

  // A close during the archive ends the session: its answer must not close or annotate the confirmation the user opened next. Releasing pending here is part of that — a superseded outcome deliberately touches nothing.
  const handleOpenChange = (next: boolean) => {
    discardSession();
    setPending(false);
    setError("");
    setOpen(next);
  };

  const archive = async () => {
    setError("");
    setPending(true);
    const outcome = await runInSession(() => archiveConnection(props.id));
    if (outcome.status === "superseded") return;
    setPending(false);
    if (outcome.status === "failed") {
      setError(errorMessage(outcome.error));
      return;
    }
    setOpen(false);
  };

  return (
    <Dialog open={open()} onOpenChange={handleOpenChange}>
      <DialogTrigger as={Button} size="sm" variant="destructive">
        Archive
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Archive “{props.displayName}”?</DialogTitle>
          <DialogDescription>
            Archiving blocks new requests and tests, and permanently discards the stored
            credential. History is kept. Restoring later requires re-entering the credential and a
            fresh connection test.
          </DialogDescription>
        </DialogHeader>
        <Show when={error() !== ""}>
          <Alert variant="destructive">
            <AlertDescription>{error()}</AlertDescription>
          </Alert>
        </Show>
        <DialogFooter>
          <Button variant="outline" disabled={pending()} onClick={() => setOpen(false)}>
            Cancel
          </Button>
          <Button variant="destructive" disabled={pending()} onClick={() => void archive()}>
            {pending() ? "Archiving…" : "Archive connection"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};
