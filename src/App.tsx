import React from 'react';
import { Routes, Route, Navigate, useNavigate } from 'react-router-dom';
import { Dashboard } from './pages/Dashboard';
import { VNCView } from './pages/VNCView';
import { StartBrowser } from './pages/StartBrowser';
import { LoginPage } from './pages/LoginPage';

interface AuthConfig {
  authEnabled: boolean;
}

const AuthContext = React.createContext<{ authEnabled: boolean; onUnauthorized: () => void }>({
  authEnabled: false,
  onUnauthorized: () => {},
});

export function useAuth() {
  return React.useContext(AuthContext);
}

function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const { authEnabled } = useAuth();
  const [checked, setChecked] = React.useState(!authEnabled);
  const [authed, setAuthed] = React.useState(false);
  const navigate = useNavigate();

  React.useEffect(() => {
    if (!authEnabled) return;
    fetch('/api/v1/status')
      .then(res => {
        if (res.status === 401) {
          navigate('/ui/login', { replace: true });
        } else {
          setAuthed(true);
        }
      })
      .catch(() => navigate('/ui/login', { replace: true }))
      .finally(() => setChecked(true));
  }, [authEnabled, navigate]);

  if (!checked) return null;
  if (authEnabled && !authed) return null;
  return <>{children}</>;
}

function AppRoutes({ authEnabled }: { authEnabled: boolean }) {
  const navigate = useNavigate();

  const onUnauthorized = React.useCallback(() => {
    navigate('/ui/login', { replace: true });
  }, [navigate]);

  return (
    <AuthContext.Provider value={{ authEnabled, onUnauthorized }}>
      <Routes>
        <Route path="/" element={<Navigate to="/ui/" replace />} />
        <Route path="/ui/login" element={<LoginPage />} />
        <Route path="/ui/" element={<ProtectedRoute><Dashboard /></ProtectedRoute>} />
        <Route path="/session/:id" element={<ProtectedRoute><VNCView /></ProtectedRoute>} />
        <Route path="/ui/start" element={<ProtectedRoute><StartBrowser /></ProtectedRoute>} />
        <Route path="*" element={<Navigate to="/ui/" replace />} />
      </Routes>
    </AuthContext.Provider>
  );
}

function App() {
  const [config, setConfig] = React.useState<AuthConfig | null>(null);

  React.useEffect(() => {
    fetch('/api/v1/auth/config')
      .then(res => res.json())
      .then((data: AuthConfig) => setConfig(data))
      .catch(() => setConfig({ authEnabled: false }));
  }, []);

  if (config === null) return null;

  return <AppRoutes authEnabled={config.authEnabled} />;
}

export default App;
