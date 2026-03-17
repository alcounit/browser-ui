import React from 'react';
import { Routes, Route, Navigate } from 'react-router-dom';
import { Dashboard } from './pages/Dashboard';
import { VNCView } from './pages/VNCView';
import { StartBrowser } from './pages/StartBrowser';

function App() {
  return (
    <Routes>
      <Route path="/" element={<Navigate to="/ui/" replace />} />
      <Route path="/ui/" element={<Dashboard />} />
      <Route path="/session/:id" element={<VNCView />} />
      <Route path="/ui/start" element={<StartBrowser />} />
      {/* Fallback */}
      <Route path="*" element={<Navigate to="/ui/" replace />} />
    </Routes>
  );
}

export default App;