export function formatUptime(startTime: string | null | undefined): string {
  if (!startTime) return '0m 0s';
  const start = new Date(startTime).getTime();
  const now = new Date().getTime();
  const diff = now - start;

  if (diff < 0) return 'Just now';

  const hours = Math.floor(diff / (1000 * 60 * 60));
  const minutes = Math.floor((diff % (1000 * 60 * 60)) / (1000 * 60));
  const seconds = Math.floor((diff % (1000 * 60)) / 1000);

  if (hours > 0) return `${hours}h ${minutes}m`;
  if (minutes > 0) return `${minutes}m ${seconds}s`;
  return `${seconds}s`;
}

export function getBrowserIcon(name: string): string {
  if (!name) return '?';
  const n = name.toLowerCase();
  if (n.includes('chrome')) return 'C';
  if (n.includes('firefox')) return 'F';
  if (n.includes('opera')) return 'O';
  if (n.includes('safari')) return 'S';
  return name[0].toUpperCase();
}