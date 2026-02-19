import type { Skill } from '../../types/game';

interface SkillsPanelProps {
  skills: Skill[];
  isConnected: boolean;
}

export const SkillsPanel: React.FC<SkillsPanelProps> = ({ skills, isConnected }) => {
  return (
    <div className="bg-spacemolt-panel border border-spacemolt-border rounded-lg p-4">
      <h3 className="font-sci-fi text-cyan-400 text-sm mb-3">SKILLS</h3>
      {!isConnected ? (
        <div className="flex flex-col items-center justify-center h-64 text-center">
          <svg className="w-12 h-12 text-gray-600 mb-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13 10V3L4 14h7v7l9-11h-7z" />
          </svg>
          <p className="text-gray-400 text-sm">Connect to an agent to view skills</p>
        </div>
      ) : skills.length === 0 ? (
        <div className="flex flex-col items-center justify-center h-64 text-center">
          <svg className="w-12 h-12 text-gray-600 mb-3" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
          <p className="text-gray-400 text-sm">No skills data available</p>
        </div>
      ) : (
        <div className="space-y-2 max-h-96 overflow-y-auto scrollbar-thin">
          {skills.map((skill, idx) => (
            <div key={idx} className="text-sm">
              <div className="flex justify-between items-center mb-1">
                <span className="text-gray-300">{skill.name}</span>
                <span className="text-yellow-400">Lv {skill.level}</span>
              </div>
              <div className="h-1.5 bg-gray-700 rounded-full overflow-hidden">
                <div
                  className="h-full bg-gradient-to-r from-cyan-500 to-blue-500 transition-all"
                  style={{ width: `${skill.xp}%` }}
                />
              </div>
              <span className="text-xs text-gray-500">{skill.xp}%</span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
};
