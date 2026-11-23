import { createRoot } from 'react-dom/client';
import { Dashboard } from './components/Dashboard';
import { ThemeProvider, DensityProvider } from '@alexgorbatchev/shadcn';
const root = document.getElementById('root');
if (!root) throw new Error('Missing application root');
createRoot(root).render(
  <ThemeProvider defaultTheme="dark">
    <DensityProvider density="comfortable">
      <Dashboard />
    </DensityProvider>
  </ThemeProvider>,
);
