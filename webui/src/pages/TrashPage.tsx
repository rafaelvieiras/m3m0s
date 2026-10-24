// Trash (D53): memories deleted by hand wait here for the grace period before
// the forget-sweep removes them for good. Restore brings one back; Delete
// forever and Empty trash remove for real, each behind a confirm dialog.
import { RotateCcw, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { PageHeader } from "@/components/layout/PageHeader";
import { KindBadge } from "@/components/memory/KindBadge";
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
import { Card, CardContent } from "@/components/ui/card";
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from "@/components/ui/empty";
import { Skeleton } from "@/components/ui/skeleton";
import { useEmptyTrash, usePurgeTrashed, useRestoreMemory, useTrash } from "@/hooks/useMemoryApi";
import { useT } from "@/i18n";
import { formatDateTime } from "@/lib/format";
import type { TrashItem } from "@/lib/schemas";

function TrashRow({ item }: { item: TrashItem }) {
  const { t, tag } = useT();
  const restore = useRestoreMemory();
  const purge = usePurgeTrashed();
  const busy = restore.isPending || purge.isPending;

  return (
    <Card size="sm">
      <CardContent className="flex flex-col gap-2">
        <p className="line-clamp-3 text-sm whitespace-pre-wrap break-words">{item.snippet}</p>
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
          <KindBadge kind={item.kind} />
          <span className="font-mono">{item.domain}</span>
          {item.deletedAt ? <span>{t("trash.deletedAt", { date: formatDateTime(item.deletedAt, tag) })}</span> : null}
          {item.purgeAt ? <span>{t("trash.purgeAt", { date: formatDateTime(item.purgeAt, tag) })}</span> : null}
        </div>
        <div className="flex gap-1">
          <Button
            variant="outline"
            size="xs"
            disabled={busy}
            onClick={() =>
              restore.mutate(item.id, {
                onSuccess: () => toast.success(t("trash.restored")),
                onError: (err) => toast.error(t("trash.restoreFailed", { message: err.message })),
              })
            }
          >
            <RotateCcw data-icon="inline-start" />
            {t("trash.restore")}
          </Button>
          <AlertDialog>
            <AlertDialogTrigger
              render={
                <Button variant="ghost" size="xs" className="text-destructive" disabled={busy}>
                  <Trash2 data-icon="inline-start" />
                  {t("trash.deleteForever")}
                </Button>
              }
            />
            <AlertDialogContent>
              <AlertDialogHeader>
                <AlertDialogTitle>{t("trash.deleteForever.title")}</AlertDialogTitle>
                <AlertDialogDescription>{t("trash.deleteForever.description")}</AlertDialogDescription>
              </AlertDialogHeader>
              <AlertDialogFooter>
                <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
                <AlertDialogAction
                  onClick={() =>
                    purge.mutate(item.id, {
                      onSuccess: () => toast.success(t("trash.deletedForever")),
                      onError: (err) => toast.error(t("trash.deleteFailed", { message: err.message })),
                    })
                  }
                >
                  {t("trash.deleteForever")}
                </AlertDialogAction>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>
        </div>
      </CardContent>
    </Card>
  );
}

function EmptyTrashButton({ count }: { count: number }) {
  const { t } = useT();
  const emptyTrash = useEmptyTrash();
  return (
    <AlertDialog>
      <AlertDialogTrigger
        render={
          <Button variant="destructive" size="sm" disabled={count === 0 || emptyTrash.isPending}>
            <Trash2 data-icon="inline-start" />
            {t("trash.empty")}
          </Button>
        }
      />
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t("trash.empty.title")}</AlertDialogTitle>
          <AlertDialogDescription>{t("trash.empty.description", { count })}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
          <AlertDialogAction
            onClick={() =>
              emptyTrash.mutate(undefined, {
                onSuccess: (res) => toast.success(t("trash.emptied", { count: res.purgedCount })),
                onError: (err) => toast.error(t("trash.deleteFailed", { message: err.message })),
              })
            }
          >
            {t("trash.empty")}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

export default function TrashPage() {
  const { t } = useT();
  const { data, isLoading, isError, error } = useTrash();

  if (isLoading) {
    return (
      <div className="flex flex-col gap-4">
        <Skeleton className="h-24" />
        <Skeleton className="h-24" />
      </div>
    );
  }

  if (isError || !data) {
    return <p className="text-sm text-destructive">{t("trash.loadFailed", { message: error?.message ?? "" })}</p>;
  }

  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title={t("trash.title")}
        subtitle={t("trash.subtitle", { days: data.graceDays })}
        actions={<EmptyTrashButton count={data.items.length} />}
      />
      {data.items.length === 0 ? (
        <Empty className="border">
          <EmptyHeader>
            <EmptyTitle>{t("trash.emptyTitle")}</EmptyTitle>
            <EmptyDescription>{t("trash.emptyDescription")}</EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <div className="flex flex-col gap-2">
          {data.items.map((item) => (
            <TrashRow key={item.id} item={item} />
          ))}
        </div>
      )}
    </div>
  );
}
