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
import { useDeleteMemory } from "@/hooks/useMemoryApi";
import { toast } from "sonner";

interface DeleteEntryDialogProps {
  id: string;
  onDeleted?: () => void;
  trigger?: React.ReactNode;
}

export function DeleteEntryDialog({ id, onDeleted, trigger }: DeleteEntryDialogProps) {
  const deleteMemory = useDeleteMemory();
  const triggerElement = trigger ?? (
    <Button variant="destructive" size="sm">
      <Trash2 data-icon="inline-start" />
      Delete
    </Button>
  );

  return (
    <AlertDialog>
      <AlertDialogTrigger render={triggerElement as React.ReactElement} />
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>Delete this memory?</AlertDialogTitle>
          <AlertDialogDescription>
            This action cannot be undone. ID <code className="font-mono">{id}</code> will no
            longer exist in the M3m0s index.
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction
            disabled={deleteMemory.isPending}
            onClick={() => {
              deleteMemory.mutate(id, {
                onSuccess: () => {
                  toast.success("Memory deleted");
                  onDeleted?.();
                },
                onError: (err) => {
                  toast.error(`Failed to delete: ${err.message}`);
                },
              });
            }}
          >
            {deleteMemory.isPending ? "Deleting…" : "Delete"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
