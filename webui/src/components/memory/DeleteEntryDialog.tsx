import { Trash2 } from "lucide-react";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { useDeleteMemory, useRestoreMemory } from "@/hooks/useMemoryApi";
import { toast } from "sonner";
import { useT } from "@/i18n";

// How long the trash keeps a deleted memory (SoftDeleteGraceDays on the server).
const TRASH_GRACE_DAYS = 30;
const UNDO_TOAST_MS = 10_000;

interface DeleteEntryDialogProps {
  id: string;
  onDeleted?: () => void;
  trigger?: React.ReactNode;
}

export function DeleteEntryDialog({ id, onDeleted, trigger }: DeleteEntryDialogProps) {
  const { t } = useT();
  const deleteMemory = useDeleteMemory();
  const restoreMemory = useRestoreMemory();
  const triggerElement = trigger ?? (
    <Button variant="destructive" size="sm">
      <Trash2 data-icon="inline-start" />
      {t("memory.delete.button")}
    </Button>
  );

  return (
    <AlertDialog>
      <AlertDialogTrigger render={triggerElement as React.ReactElement} />
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t("memory.delete.title")}</AlertDialogTitle>
          <AlertDialogDescription>
            {t("memory.delete.descriptionBefore")}<code className="font-mono">{id}</code>{t("memory.delete.descriptionAfter", { days: TRASH_GRACE_DAYS })}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t("memory.delete.cancel")}</AlertDialogCancel>
          <AlertDialogAction
            disabled={deleteMemory.isPending}
            onClick={() => {
              deleteMemory.mutate(id, {
                onSuccess: () => {
                  toast.success(t("memory.delete.success"), {
                    duration: UNDO_TOAST_MS,
                    action: {
                      label: t("memory.delete.undo"),
                      onClick: () =>
                        restoreMemory.mutate(id, {
                          onSuccess: () => toast.success(t("memory.delete.undone")),
                          onError: (err) => toast.error(t("memory.delete.undoFailed", { message: err.message })),
                        }),
                    },
                  });
                  onDeleted?.();
                },
                onError: (err) => {
                  toast.error(t("memory.delete.failed", { message: err.message }));
                },
              });
            }}
          >
            {deleteMemory.isPending ? t("memory.delete.deleting") : t("memory.delete.button")}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
