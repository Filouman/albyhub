import { Globe } from "lucide-react";
import { Link, useNavigate, useParams } from "react-router-dom";
import AppHeader from "src/components/AppHeader";
import ExternalLink from "src/components/ExternalLink";
import { AppleIcon } from "src/components/icons/Apple";
import { ChromeIcon } from "src/components/icons/Chrome";
import { FirefoxIcon } from "src/components/icons/Firefox";
import { NostrWalletConnectIcon } from "src/components/icons/NostrWalletConnectIcon";
import { PlayStoreIcon } from "src/components/icons/PlayStore";
import { ZapStoreIcon } from "src/components/icons/ZapStore";
import { suggestedApps } from "src/components/SuggestedAppData";
import { Button } from "src/components/ui/button";
import {
  Card,
  CardContent,
  CardFooter,
  CardHeader,
  CardTitle,
} from "src/components/ui/card";

export function AppStoreDetail() {
  const { appId } = useParams() as { appId: string };
  const app = suggestedApps.find((x) => x.id === appId);
  const navigate = useNavigate();

  if (!app) {
    navigate("/appstore");
    return;
  }

  return (
    <div className="grid gap-5">
      <AppHeader
        title={
          <>
            <div className="flex flex-row items-center">
              <img src={app.logo} className="w-14 h-14 rounded-lg mr-4" />
              <div className="flex flex-col">
                <div>{app.title}</div>
                <div className="text-sm font-normal text-muted-foreground">
                  {app.description}
                </div>
              </div>
            </div>
          </>
        }
        description=""
        contentRight={
          <Link to={`/apps/new?app=${app.id}`}>
            <Button>
              <NostrWalletConnectIcon className="w-4 h-4 mr-2" />
              Connect to {app.title}
            </Button>
          </Link>
        }
      />
      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
        {(app.appleLink ||
          app.playLink ||
          app.zapStoreLink ||
          app.chromeLink ||
          app.firefoxLink) && (
          <Card>
            <CardHeader>
              <CardTitle>Get This App</CardTitle>
            </CardHeader>
            <CardFooter className="flex flex-row gap-2">
              {app.playLink && (
                <ExternalLink to={app.playLink}>
                  <Button variant="outline">
                    <PlayStoreIcon className="w-4 h-4 mr-2" />
                    Play Store
                  </Button>
                </ExternalLink>
              )}
              {app.appleLink && (
                <ExternalLink to={app.appleLink}>
                  <Button variant="outline">
                    <AppleIcon className="w-4 h-4 mr-2" />
                    App Store
                  </Button>
                </ExternalLink>
              )}
              {app.zapStoreLink && (
                <ExternalLink to="https://zap.store/download">
                  <Button variant="outline">
                    <ZapStoreIcon className="w-4 h-4 mr-2" />
                    zap.store
                  </Button>
                </ExternalLink>
              )}
              {app.chromeLink && (
                <ExternalLink to={app.chromeLink}>
                  <Button variant="outline">
                    <ChromeIcon className="w-4 h-4 mr-2" />
                    Chrome Web Store
                  </Button>
                </ExternalLink>
              )}
              {app.firefoxLink && (
                <ExternalLink to={app.firefoxLink}>
                  <Button variant="outline">
                    <FirefoxIcon className="w-4 h-4 mr-2" />
                    Firefox Add-Ons
                  </Button>
                </ExternalLink>
              )}
            </CardFooter>
          </Card>
        )}
        {app.webLink && (
          <Card>
            <CardHeader>
              <CardTitle>Links</CardTitle>
            </CardHeader>
            <CardFooter className="flex flex-row gap-2">
              {app.webLink && (
                <ExternalLink to={app.webLink}>
                  <Button variant="outline">
                    <Globe className="w-4 h-4 mr-2" />
                    Website
                  </Button>
                </ExternalLink>
              )}
            </CardFooter>
          </Card>
        )}
      </div>
      <Card>
        <CardHeader>
          <CardTitle>How to Connect</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          {app.guide || (
            <ul className="list-inside list-decimal">
              <li>Install the app</li>
              <li>
                Click{" "}
                <Link to={`/apps/new?app=${appId}`}>
                  <Button variant="link" className="px-0">
                    Connect to {app.title}
                  </Button>
                </Link>
              </li>
              <li>Open the Alby Go app on your mobile and scan the QR code</li>
            </ul>
          )}
        </CardContent>
      </Card>
    </div>
  );
}