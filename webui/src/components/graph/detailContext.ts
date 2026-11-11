import { createContext } from 'react';
import type { DetailLevel } from '../../utils/graphDetail';

export const GraphDetailContext = createContext<DetailLevel>('full');
