import type { Skill } from '../../types/game';

interface SkillsPanelProps {
  skills: Skill[];
}

export const SkillsPanel: React.FC<SkillsPanelProps> = ({ skills }) => {
  return (
    <div className="bg-spacemolt-panel border border-spacemolt-border rounded-lg p-4">
      <h3 className="font-sci-fi text-cyan-400 text-sm mb-3">SKILLS</h3>
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
    </div>
  );
};
