import { useState, useEffect } from 'react';
import type { Mission, ActiveMission, MissionTab } from '../../types/game';

interface MissionsPanelProps {
  availableMissions: Mission[];
  activeMissions: ActiveMission[];
  onGetMissions: () => void;
  onGetActiveMissions: () => void;
  onRefresh?: () => void;
  onAcceptMission: (missionId: string) => void;
  onAbandonMission: (missionId: string) => void;
  onCompleteMission: (missionId: string) => void;
}

export const MissionsPanel: React.FC<MissionsPanelProps> = ({
  availableMissions,
  activeMissions,
  onGetMissions,
  onGetActiveMissions,
  onRefresh,
  onAcceptMission,
  onAbandonMission,
  onCompleteMission,
}) => {
  const [activeTab, setActiveTab] = useState<MissionTab>('AVAILABLE');
  const [selectedMission, setSelectedMission] = useState<Mission | ActiveMission | null>(null);

  // Fetch missions when component mounts
  useEffect(() => {
    onGetMissions();
    onGetActiveMissions();
  }, []);

  const getMissionsForTab = (): (Mission | ActiveMission)[] => {
    switch (activeTab) {
      case 'AVAILABLE':
        return availableMissions;
      case 'ACTIVE':
        return activeMissions;
      case 'COMPLETED':
        // For now, completed missions are active missions that are completed
        return activeMissions.filter(m => m.objectives.every(obj => obj.completed));
      default:
        return [];
    }
  };

  const missions = getMissionsForTab();

  const handleMissionClick = (mission: Mission | ActiveMission) => {
    setSelectedMission(mission);
  };

  const handleAcceptMission = () => {
    if (selectedMission && 'mission_id' in selectedMission) {
      onAcceptMission(selectedMission.mission_id);
      setSelectedMission(null);
    }
  };

  const handleAbandonMission = () => {
    if (selectedMission && 'mission_id' in selectedMission) {
      onAbandonMission(selectedMission.mission_id);
      setSelectedMission(null);
    }
  };

  const handleCompleteMission = () => {
    if (selectedMission && 'mission_id' in selectedMission) {
      onCompleteMission(selectedMission.mission_id);
      setSelectedMission(null);
    }
  };

  const canCompleteMission = (mission: ActiveMission) => {
    return mission.can_complete || mission.objectives.every(obj => obj.completed);
  };

  return (
    <div className="bg-spacemolt-panel border border-spacemolt-border rounded-lg p-4">
      <div className="flex items-center justify-between mb-4">
        <h3 className="font-sci-fi text-cyan-400">MISSIONS</h3>
        {onRefresh && (
          <button
            onClick={() => onRefresh()}
            className="text-xs text-gray-400 hover:text-cyan-400 border border-gray-600 hover:border-cyan-600 px-2 py-1 rounded"
          >
            REFRESH
          </button>
        )}
      </div>

      {/* Tab navigation */}
      <div className="flex gap-2 mb-4">
        {(['AVAILABLE', 'ACTIVE', 'COMPLETED'] as MissionTab[]).map((tab) => (
          <button
            key={tab}
            onClick={() => {
              setActiveTab(tab);
              setSelectedMission(null);
              if (tab === 'AVAILABLE') onGetMissions();
              if (tab === 'ACTIVE' || tab === 'COMPLETED') onGetActiveMissions();
            }}
            className={`px-4 py-2 rounded text-sm transition-colors ${
              activeTab === tab
                ? 'bg-cyan-600 text-white'
                : 'bg-gray-700 text-gray-400 hover:bg-gray-600'
            }`}
          >
            {tab} ({tab === 'AVAILABLE' ? availableMissions.length : tab === 'ACTIVE' ? `${activeMissions.length}/5` : activeMissions.length})
          </button>
        ))}
      </div>

      {/* Active mission limit warning */}
      {activeTab === 'ACTIVE' && activeMissions.length >= 5 && (
        <div className="mb-4 p-3 bg-yellow-900/20 border border-yellow-700 rounded-lg">
          <div className="flex items-center gap-2">
            <svg className="w-5 h-5 text-yellow-500" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
            </svg>
            <span className="text-sm text-yellow-300">Active mission limit reached (5/5). Abandon a mission to accept more.</span>
          </div>
        </div>
      )}

      {/* Two-pane layout */}
      <div className="flex gap-4">
        {/* Left panel - Mission list */}
        <div className="w-1/2">
          <div className="text-sm text-gray-400 mb-3 uppercase">
            {activeTab} MISSIONS:
          </div>
          <div className="space-y-2 max-h-96 overflow-y-auto scrollbar-thin">
            {missions.length === 0 ? (
              <div className="text-center text-gray-500 py-8 bg-gray-800/50 rounded-lg border border-gray-700">
                {activeTab === 'AVAILABLE'
                  ? 'No missions available at this station'
                  : activeTab === 'ACTIVE'
                  ? 'No active missions'
                  : 'No completed missions'}
              </div>
            ) : (
              missions.map((mission) => (
                <div
                  key={mission.mission_id}
                  onClick={() => handleMissionClick(mission)}
                  className={`p-3 rounded cursor-pointer transition-colors border ${
                    selectedMission?.mission_id === mission.mission_id
                      ? 'bg-cyan-900/30 border-cyan-600'
                      : 'bg-gray-800/50 border-gray-700 hover:bg-gray-800 hover:border-gray-600'
                  }`}
                >
                  <div className="flex justify-between items-start mb-2">
                    <h4 className="font-semibold text-gray-200 text-sm">{mission.title}</h4>
                    <span className={`text-xs px-2 py-1 rounded ${
                      mission.difficulty === 'easy' ? 'bg-green-900 text-green-300' :
                      mission.difficulty === 'medium' ? 'bg-yellow-900 text-yellow-300' :
                      mission.difficulty === 'hard' ? 'bg-red-900 text-red-300' :
                      'bg-gray-700 text-gray-300'
                    }`}>
                      {mission.difficulty}
                    </span>
                  </div>
                  <p className="text-xs text-gray-400 mb-2 line-clamp-2">{mission.description}</p>
                  <div className="flex justify-between items-center text-xs">
                    <span className="text-cyan-400">{mission.giver_name}</span>
                    <span className="text-gray-500">{mission.estimated_time}</span>
                  </div>
                  {'progress' in mission && (
                    <div className="mt-2">
                      <div className="h-1.5 bg-gray-700 rounded-full overflow-hidden">
                        <div
                          className="h-full bg-cyan-500 transition-all"
                          style={{ width: `${mission.progress}%` }}
                        />
                      </div>
                      <span className="text-xs text-gray-500">{mission.progress.toFixed(0)}% complete</span>
                    </div>
                  )}
                </div>
              ))
            )}
          </div>
        </div>

        {/* Right panel - Mission details */}
        <div className="w-1/2">
          {selectedMission ? (
            <div className="bg-gray-800/50 rounded-lg border border-gray-700 p-4">
              <div className="flex justify-between items-start mb-4">
                <h4 className="font-semibold text-gray-200 text-lg">{selectedMission.title}</h4>
                <span className={`text-xs px-2 py-1 rounded ${
                  selectedMission.difficulty === 'easy' ? 'bg-green-900 text-green-300' :
                  selectedMission.difficulty === 'medium' ? 'bg-yellow-900 text-yellow-300' :
                  selectedMission.difficulty === 'hard' ? 'bg-red-900 text-red-300' :
                  'bg-gray-700 text-gray-300'
                }`}>
                  {selectedMission.difficulty}
                </span>
              </div>

              <div className="mb-4">
                <p className="text-sm text-gray-300 mb-2">{selectedMission.description}</p>
                <div className="flex gap-4 text-xs text-gray-400">
                  <span>📋 {selectedMission.type}</span>
                  <span>⏱️ {selectedMission.estimated_time}</span>
                  {selectedMission.is_repeatable && <span>🔄 Repeatable</span>}
                </div>
              </div>

              <div className="mb-4">
                <h5 className="text-sm font-semibold text-cyan-400 mb-2">GIVEN BY</h5>
                <p className="text-sm text-gray-300">
                  {selectedMission.giver_name} - {selectedMission.giver_title}
                </p>
              </div>

              <div className="mb-4">
                <h5 className="text-sm font-semibold text-cyan-400 mb-2">OBJECTIVES</h5>
                <div className="space-y-2">
                  {selectedMission.objectives.map((objective, idx) => (
                    <div
                      key={objective.id || idx}
                      className={`p-2 rounded text-sm ${
                        objective.completed
                          ? 'bg-green-900/20 border border-green-700'
                          : 'bg-gray-700/30 border border-gray-600'
                      }`}
                    >
                      <div className="flex justify-between items-center">
                        <span className={objective.completed ? 'text-green-400 line-through' : 'text-gray-300'}>
                          {objective.description}
                        </span>
                        <span className="text-xs text-gray-400">
                          {objective.current}/{objective.target}
                        </span>
                      </div>
                    </div>
                  ))}
                </div>
              </div>

              <div className="mb-4">
                <h5 className="text-sm font-semibold text-cyan-400 mb-2">REWARDS</h5>
                <div className="space-y-1">
                  {selectedMission.rewards.map((reward, idx) => (
                    <div key={idx} className="text-sm text-gray-300">
                      {reward.type === 'credits' && (
                        <span>💰 {reward.amount?.toLocaleString()} credits</span>
                      )}
                      {reward.type === 'items' && (
                        <span>📦 {reward.item_name} x{reward.amount}</span>
                      )}
                      {reward.type === 'ships' && (
                        <span>🚀 {reward.item_name}</span>
                      )}
                      {reward.type === 'faction' && (
                        <span>🏛️ {reward.description}</span>
                      )}
                      {reward.type === 'skill' && (
                        <span>📊 {reward.skill_id} +{reward.skill_xp} XP</span>
                      )}
                    </div>
                  ))}
                </div>
              </div>

              {/* Action buttons */}
              <div className="flex gap-2">
                {activeTab === 'AVAILABLE' && (
                  <button
                    onClick={handleAcceptMission}
                    disabled={activeMissions.length >= 5}
                    className={`flex-1 px-4 py-2 rounded text-sm transition-colors ${
                      activeMissions.length >= 5
                        ? 'bg-gray-600 text-gray-400 cursor-not-allowed'
                        : 'bg-cyan-600 hover:bg-cyan-500 text-white'
                    }`}
                  >
                    {activeMissions.length >= 5 ? 'Mission Limit Full' : 'Accept Mission'}
                  </button>
                )}
                {activeTab === 'ACTIVE' && 'is_abandonable' in selectedMission && selectedMission.is_abandonable && (
                  <button
                    onClick={handleAbandonMission}
                    className="flex-1 bg-red-600 hover:bg-red-500 text-white px-4 py-2 rounded text-sm transition-colors"
                  >
                    Abandon Mission
                  </button>
                )}
                {activeTab === 'ACTIVE' && 'can_complete' in selectedMission && canCompleteMission(selectedMission) && (
                  <button
                    onClick={handleCompleteMission}
                    className="flex-1 bg-green-600 hover:bg-green-500 text-white px-4 py-2 rounded text-sm transition-colors"
                  >
                    Complete Mission
                  </button>
                )}
              </div>
            </div>
          ) : (
            <div className="bg-gray-800/50 rounded-lg border border-gray-700 p-8 flex flex-col items-center justify-center h-full">
              <svg className="w-16 h-16 text-gray-600 mb-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2m-6 9l2 2 4-4" />
              </svg>
              <p className="text-gray-400 text-sm text-center">Select a mission to view details</p>
            </div>
          )}
        </div>
      </div>
    </div>
  );
};
