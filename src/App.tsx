import React from 'react';
import { Routes, Route, Navigate } from 'react-router-dom';
import { Dashboard } from './pages/Dashboard';
import { VNCView } from './pages/VNCView';

function App() {
  return (
    <Routes>
      <Route path="/" element={<Navigate to="/ui/" replace />} />
      <Route path="/ui/" element={<Dashboard />} />
      <Route path="/session/:id" element={<VNCView />} />
      {/* Fallback */}
      <Route path="*" element={<Navigate to="/ui/" replace />} />
    </Routes>
  );
}

export default App;