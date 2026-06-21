// ============================================================
// Tower SPA — Settings hub page
// Route: /settings (adminOnly)
// Grid of cards linking to admin/config sub-pages.
// ============================================================
import { useNavigate } from 'react-router-dom';

interface SettingsCard {
  path: string;
  icon: string;
  title: string;
  description: string;
}

const CARDS: SettingsCard[] = [
  {
    path: '/policies',
    icon: '⛨',
    title: '封控策略',
    description: '配置封号触发规则与自动封控策略',
  },
  {
    path: '/slots',
    icon: '⏱',
    title: '时段槽位',
    description: '管理调度时段与并发槽位分配',
  },
  {
    path: '/desired',
    icon: '⇌',
    title: '配置对账',
    description: '对比期望配置与实际运行状态',
  },
  {
    path: '/keys',
    icon: '🔑',
    title: '调度密钥',
    description: '管理用于调度层鉴权的 API 密钥',
  },
  {
    path: '/ban-analysis',
    icon: '⚠',
    title: '封号分析',
    description: '分析封号模式与历史封控数据',
  },
  {
    path: '/users',
    icon: '👤',
    title: '用户',
    description: '管理控制台用户与权限角色',
  },
];

export default function Settings() {
  const navigate = useNavigate();

  return (
    <div className="p-6 max-w-4xl mx-auto">
      <h1 className="text-xl font-semibold text-ink mb-1">设置</h1>
      <p className="text-sm text-muted mb-6">管理系统配置与运营策略</p>

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
        {CARDS.map((card) => (
          <button
            key={card.path}
            onClick={() => navigate(card.path)}
            className="flex flex-col gap-3 p-5 rounded-xl border border-line bg-surface
                       text-left transition hover:border-accent hover:bg-accent/5 group"
          >
            <span className="text-2xl">{card.icon}</span>
            <div>
              <div className="text-sm font-semibold text-ink group-hover:text-accent transition">
                {card.title}
              </div>
              <div className="text-xs text-muted mt-0.5 leading-relaxed">
                {card.description}
              </div>
            </div>
          </button>
        ))}
      </div>
    </div>
  );
}
