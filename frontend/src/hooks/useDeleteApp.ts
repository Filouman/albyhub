import React from "react";
import { useToast } from "src/components/ui/use-toast";

import { handleRequestError } from "src/utils/handleRequestError";
import { request } from "src/utils/request";

export function useDeleteApp(onSuccess?: (appPubkey: string) => void) {
  const [isDeleting, setDeleting] = React.useState(false);
  const { toast } = useToast();

  const deleteApp = React.useCallback(
    async (appPubkey: string) => {
      setDeleting(true);
      try {
        await request(`/api/apps/${appPubkey}`, {
          method: "DELETE",
          headers: {
            "Content-Type": "application/json",
          },
        });
        toast({ title: "Connection deleted" });
        if (onSuccess) {
          onSuccess(appPubkey);
        }
      } catch (error) {
        await handleRequestError(toast, "Failed to delete connection", error);
      } finally {
        setDeleting(false);
      }
    },
    [onSuccess, toast]
  );

  return React.useMemo(
    () => ({ deleteApp, isDeleting }),
    [deleteApp, isDeleting]
  );
}
