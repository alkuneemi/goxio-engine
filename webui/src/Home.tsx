import { Layout } from './components';
import PortalGate from './components/portal/PortalGate';

export default function Home() {
  return (
    <PortalGate>
      <Layout />
    </PortalGate>
  );
}
