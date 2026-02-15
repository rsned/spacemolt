import type { Recipe } from '../../types/game';
import { useState } from 'react';

interface WorkshopPanelProps {
  recipes: Recipe[];
}

export const WorkshopPanel: React.FC<WorkshopPanelProps> = ({ recipes }) => {
  const [showAll, setShowAll] = useState(false);

  return (
    <div className="bg-spacemolt-panel border border-spacemolt-border rounded-lg p-4">
      <h3 className="font-sci-fi text-cyan-400 mb-4">WORKSHOP</h3>

      <div className="flex gap-2 mb-4">
        <button
          onClick={() => setShowAll(false)}
          className={`px-4 py-2 rounded text-sm ${
            !showAll ? 'bg-cyan-600 text-white' : 'bg-gray-700 text-gray-400 hover:bg-gray-600'
          }`}
        >
          Available Recipes
        </button>
        <button
          onClick={() => setShowAll(true)}
          className={`px-4 py-2 rounded text-sm ${
            showAll ? 'bg-cyan-600 text-white' : 'bg-gray-700 text-gray-400 hover:bg-gray-600'
          }`}
        >
          All Recipes
        </button>
      </div>

      <div className="text-sm text-gray-400 mb-3">AVAILABLE (you have skills + materials):</div>

      {recipes.map((recipe, idx) => (
        <div key={idx} className="bg-gray-800 rounded-lg p-4 mb-3 border border-gray-700">
          <div className="flex justify-between items-start mb-2">
            <div>
              <div className="font-semibold text-lg">{recipe.name}</div>
              <div className="text-sm text-gray-400">
                Category: {recipe.category} | Skill: {recipe.skillReq}
              </div>
            </div>
          </div>

          <div className="grid grid-cols-2 gap-4 text-sm mb-3">
            <div>
              <span className="text-gray-400">Input:</span>
              <div className="text-red-300">
                {recipe.input.map((i, idx) => (
                  <span key={idx}>
                    {idx > 0 && ', '}
                    {i.count}× {i.name}
                  </span>
                ))}
              </div>
            </div>
            <div>
              <span className="text-gray-400">Output:</span>
              <div className="text-green-300">
                {recipe.output.map((o, idx) => (
                  <span key={idx}>
                    {idx > 0 && ', '}
                    {o.count}× {o.name}
                  </span>
                ))}
              </div>
            </div>
          </div>

          <div className="text-sm text-gray-400 mb-2">
            You have: 45× ore_iron (enough for {recipe.maxCraftable}×)
          </div>

          <div className="flex gap-2">
            {[1, 5, 10].map((n) => (
              <button key={n} className="bg-cyan-700 hover:bg-cyan-600 px-4 py-1 rounded text-sm">
                {n}x
              </button>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
};
