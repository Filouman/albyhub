import { toast } from "src/components/ui/use-toast";
import { usePeers } from "src/hooks/usePeers";
import { Peer } from "src/types";
import { request } from "src/utils/request";
import {
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "./ui/alert-dialog";

type Props = {
  peer: Peer;
  name: string | undefined;
};

export function DisconnectPeerDialogContent({ peer, name }: Props) {
  const { mutate: reloadPeers } = usePeers();

  async function disconnectPeer() {
    try {
      console.info(`Disconnecting from ${peer.nodeId}`);

      await request(`/api/peers/${peer.nodeId}`, {
        method: "DELETE",
        headers: {
          "Content-Type": "application/json",
        },
      });
      toast({
        title: "Successfully disconnected from peer",
        description: peer.nodeId,
      });
      await reloadPeers();
    } catch (e) {
      toast({
        variant: "destructive",
        title: "Failed to disconnect peer",
        description: "" + e,
      });
      console.error(e);
    }
  }

  return (
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>Disconnect Peer</AlertDialogTitle>
        <AlertDialogDescription>
          <div>
            <p>
              Are you sure you wish to disconnect from {name || "this peer"}?
            </p>
            <p className="text-primary font-medium mt-4">Peer Pubkey</p>
            <p className="break-all">{peer.nodeId}</p>
          </div>
        </AlertDialogDescription>
      </AlertDialogHeader>
      <AlertDialogFooter>
        <AlertDialogCancel>Cancel</AlertDialogCancel>
        <AlertDialogAction onClick={disconnectPeer}>Confirm</AlertDialogAction>
      </AlertDialogFooter>
    </AlertDialogContent>
  );
}