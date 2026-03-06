import React from "react";
import { useSelector } from "react-redux";

import type { GlobalState } from "@mattermost/types/store";

import { getCurrentChannelId } from "mattermost-redux/selectors/entities/common";

import manifest from "../../manifest";

interface Props {
    channelId?: string;
}

const MeetButton: React.FC<Props> = ({ channelId: propsChannelId }) => {
    const stateChannelId = useSelector((state: GlobalState) =>
        getCurrentChannelId(state),
    );
    const channelId = propsChannelId || stateChannelId;

    const handleClick = async () => {
        try {
            const response = await fetch(
                `/plugins/${manifest.id}/api/v1/create?channel_id=${channelId}`,
                {
                    method: "POST",
                    headers: {
                        "Content-Type": "application/json",
                    },
                },
            );

            if (response.status === 401) {
                // Redirect to login if not authenticated
                window.open(`/plugins/${manifest.id}/oauth2/login`, "_blank");
                return;
            }

            if (!response.ok) {
                throw new Error("Failed to create meeting");
            }

            const data = await response.json();

            // Open the meeting link in a new tab
            window.open(data.meet_url, "_blank");
        } catch (error) {
            // eslint-disable-next-line no-console
            console.error("Error creating meeting:", error);
        }
    };

    return (
        <button
            className="channel-header__icon"
            onClick={handleClick}
            style={{
                background: "none",
                border: "none",
                cursor: "pointer",
                display: "flex",
                alignItems: "center",
                justifyContent: "center",
            }}
            title="Create Google Meet"
        >
            <i className="fa fa-video-camera" />
        </button>
    );
};

export default MeetButton;
