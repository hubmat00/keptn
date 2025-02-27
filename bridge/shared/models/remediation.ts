import { Sequence, SequenceStage } from './sequence';
import { IRemediationAction } from './remediation-action';

export class Remediation extends Sequence {
  stages: (SequenceStage & {
    actions: IRemediationAction[];
  })[] = [];

  public static fromJSON(data: unknown): Remediation {
    return Object.assign(new this(), data);
  }
}
